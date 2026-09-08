package vm

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"

	"vm-runner/internal/storage"
)

// QEMUManager is responsible for managing a single QEMU VM instance.
type QEMUManager struct {
	Config      storage.VMConfig
	RuntimeDir  string
	overlayPath string
	Cmd         *exec.Cmd
	PTY io.ReadWriteCloser
	OutputChan  chan []byte
	ErrorChan   chan error
	stopChan    chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
	VNCPort     int
	logFile     *os.File
}

// NewQEMUManager creates a new manager for a QEMU VM.
func NewQEMUManager(config storage.VMConfig, runtimeDir string) *QEMUManager {
	return &QEMUManager{
		Config:     config,
		RuntimeDir: runtimeDir,
		OutputChan: make(chan []byte, 1024),
		ErrorChan:  make(chan error, 1),
		stopChan:   make(chan struct{}),
	}
}

// Start launches the QEMU VM.
func (qm *QEMUManager) Start() error {
	commonArgs := []string{"-m", fmt.Sprintf("%d", qm.Config.MemoryMB)}
	if qm.Config.CPUs > 0 {
		commonArgs = append(commonArgs, "-smp", fmt.Sprintf("%d", qm.Config.CPUs))
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Stat("/dev/kvm"); err == nil {
			commonArgs = append(commonArgs, "-enable-kvm", "-cpu", "host")
		} else {
			// Container environments (Render, Railway, etc.) have no nested virt.
			// Multi-threaded TCG is significantly faster than the default single-threaded
			// interpreter – cuts Alpine boot from ~3 min to ~90 s.
			commonArgs = append(commonArgs, "-accel", "tcg,thread=multi")
		}
	} else {
		// on Windows, prefer WHPX hardware acceleration for fast boots, but fallback to TCG
		commonArgs = append(commonArgs, "-accel", "whpx", "-accel", "tcg")
	}

	var winSerialPort int
	if qm.Config.DisplayType == "vnc" {
		vncPort, wsPort, err := qm.findFreePorts()
		if err != nil {
			log.Printf("failed to find free ports for vnc: %v", err)
			return fmt.Errorf("failed to find free ports: %w", err)
		}
		qm.VNCPort = wsPort
		vncDisplay := vncPort - 5900
		commonArgs = append(commonArgs,
			"-vnc", fmt.Sprintf(":%d,websocket=%d", vncDisplay, wsPort),
			"-vga", "std",
			"-usb",
			"-device", "usb-tablet", // Absolute pointer for perfect mouse sync
			"-device", "virtio-serial-pci",
			"-device", "virtserialport,chardev=ch0,name=com.redhat.spice.0",
			"-chardev", "qemu-vdagent,id=ch0,name=vdagent,clipboard=on",
		)
		if runtime.GOOS == "windows" {
			winSerialPort, _ = qm.findSingleFreePort()
			commonArgs = append(commonArgs, "-serial", fmt.Sprintf("tcp:127.0.0.1:%d,server,nowait", winSerialPort))
		} else {
			commonArgs = append(commonArgs, "-serial", "mon:stdio")
		}
	} else {
		if runtime.GOOS == "windows" {
			winSerialPort, _ = qm.findSingleFreePort()
			commonArgs = append(commonArgs, "-display", "none", "-serial", fmt.Sprintf("tcp:127.0.0.1:%d,server,nowait", winSerialPort))
		} else {
			commonArgs = append(commonArgs, "-nographic", "-serial", "mon:stdio")
		}
	}

	args := append([]string{}, commonArgs...)
	args = append(args, "-net", "none")
	if qm.Config.ImagePath != "" {
		imageArg := qm.Config.ImagePath
		if qm.Config.ImageFormat == "qcow2" || qm.Config.ImageFormat == "raw" {
			if qm.RuntimeDir != "" {
				qm.overlayPath = filepath.Join(qm.RuntimeDir, "overlay.qcow2")
				if err := qm.createOverlay(); err == nil {
					imageArg = qm.overlayPath
				}
			}
			args = append(args, "-drive", fmt.Sprintf("file=%s,if=virtio", imageArg))
		} else {
			args = append(args, "-hda", imageArg)
		}
	}

	// If a runtime directory is provided, expose it to the guest via 9p/virtfs
	// so the guest can read the session seed (mounted by the guest at boot).
	if qm.RuntimeDir != "" && runtime.GOOS != "windows" {
		args = append(args, "-virtfs", fmt.Sprintf("local,path=%s,mount_tag=vmrunner,security_model=none,id=vmrun0", qm.RuntimeDir))
	}
	qemuExe := "qemu-system-x86_64"
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath(qemuExe); err != nil {
			if _, err := os.Stat(`C:\Program Files\qemu\qemu-system-x86_64.exe`); err == nil {
				qemuExe = `C:\Program Files\qemu\qemu-system-x86_64.exe`
			}
		}
	}
	qm.Cmd = exec.Command(qemuExe, args...)
	log.Println("starting qemu with args:", qm.Cmd.Args)
	if runtime.GOOS == "windows" {
		if err := qm.Cmd.Start(); err != nil {
			return fmt.Errorf("failed to start qemu: %w", err)
		}
		// Connect to the TCP serial port
		conn, err := connectWithRetry(fmt.Sprintf("127.0.0.1:%d", winSerialPort), 20, 100*time.Millisecond)
		if err != nil {
			_ = qm.Cmd.Process.Kill()
			return fmt.Errorf("failed to connect to qemu serial: %w", err)
		}
		qm.PTY = conn
	} else {
		// Create a pty for the QEMU process so the guest sees a real tty.
		ptmx, err := pty.Start(qm.Cmd)
		if err != nil {
			log.Printf("failed to start qemu with pty: %v", err)
			return fmt.Errorf("failed to start qemu with pty: %w", err)
		}
		qm.PTY = ptmx
	}

	// Open a log file in the runtime dir to persist VM output (helps debug
	// immediate exits). streamOutput will append to this file.
	if qm.RuntimeDir != "" {
		_ = os.MkdirAll(qm.RuntimeDir, 0o755)
		lf, err := os.OpenFile(filepath.Join(qm.RuntimeDir, "qemu.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			qm.logFile = lf
		} else {
			log.Printf("warning: failed to open qemu log file: %v", err)
		}
	}

	if qm.Config.DisplayType == "vnc" {
		// Give QEMU a moment to initialize the VNC/WebSocket server
		time.Sleep(500 * time.Millisecond)
	}

	qm.wg.Add(1)
	go qm.streamOutput(qm.PTY)

	// Monitor process exit. Make a channel to observe an early exit so callers
	// can treat immediate failures as start errors.
	exitChan := make(chan error, 1)
	go func() {
		err := qm.Cmd.Wait()
		exitChan <- err
	}()

	// Background goroutine to close channels once streamOutput finishes and
	// report any process error to ErrorChan.
	go func() {
		qm.wg.Wait()
		close(qm.OutputChan)
		err := <-exitChan
		if err != nil {
			// Try to include the last few lines of qemu.log to aid debugging.
			var tail []byte
			if qm.logFile != nil {
				_ = qm.logFile.Close()
				if data, readErr := os.ReadFile(filepath.Join(qm.RuntimeDir, "qemu.log")); readErr == nil {
					if len(data) > 4096 {
						tail = data[len(data)-4096:]
					} else {
						tail = data
					}
				}
			}
			if len(tail) > 0 {
				qm.ErrorChan <- fmt.Errorf("vm process exited with error: %w; qemu.log tail:\n% s", err, string(tail))
			} else {
				qm.ErrorChan <- fmt.Errorf("vm process exited with error: %w", err)
			}
		}
		close(qm.ErrorChan)
	}()

	// Allow a short grace period for QEMU to fail early. If it exits quickly
	// we surface the error synchronously so the HTTP handler won't return 201.
	select {
	case err := <-exitChan:
		if err != nil {
			log.Printf("qemu exited immediately: %v", err)
			return fmt.Errorf("qemu exited immediately: %w", err)
		}
		log.Printf("qemu exited immediately without error")
		return fmt.Errorf("qemu exited immediately")
	case <-time.After(300 * time.Millisecond):
		// started successfully (or at least survived initial window)
	}

	log.Printf("qemu pid %d started", qm.Cmd.Process.Pid)
	return nil
}

// Stop terminates the QEMU VM.
func (qm *QEMUManager) Stop() error {
	qm.stopOnce.Do(func() {
		close(qm.stopChan)
	})
	if qm.PTY != nil {
		_ = qm.PTY.Close()
	}
	if qm.Cmd != nil && qm.Cmd.Process != nil {
		if err := qm.Cmd.Process.Kill(); err != nil {
			// Treat already finished processes as non-errors.
			if errors.Is(err, os.ErrProcessDone) {
				return nil
			}
			return err
		}
	}
	return nil
}

// SendInput sends a string to the VM's standard input.
func (qm *QEMUManager) SendInput(input string) (int, error) {
	if qm.PTY == nil {
		return 0, fmt.Errorf("pty not initialized")
	}
	// Map DEL (0x7f) to BS (0x08) which many guest ttys expect for backspace.
	b := []byte(input)
	for i := range b {
		if b[i] == 0x7f {
			b[i] = 0x08
		}
	}
	return qm.PTY.Write(b)
}

// streamOutput reads from a reader and sends lines to the output channel.
func (qm *QEMUManager) streamOutput(reader io.Reader) {
	defer qm.wg.Done()
	buf := make([]byte, 4096)
	for {
		select {
		case <-qm.stopChan:
			return
		default:
		}
		n, err := reader.Read(buf)
		if n > 0 {
			// copy bytes because buf is reused
			out := make([]byte, n)
			copy(out, buf[:n])
			select {
			case qm.OutputChan <- out:
			case <-qm.stopChan:
				return
			}
			// Also write a copy to the persistent qemu log file when available.
			if qm.logFile != nil {
				_, _ = qm.logFile.Write(out)
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			qm.ErrorChan <- err
			return
		}
	}
}

func (qm *QEMUManager) createOverlay() error {
	if qm.Config.ImagePath == "" || qm.overlayPath == "" {
		return nil
	}
	_ = os.MkdirAll(qm.RuntimeDir, 0o755)
	qemuImg := "qemu-img"
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath(qemuImg); err != nil {
			if _, err := os.Stat(`C:\Program Files\qemu\qemu-img.exe`); err == nil {
				qemuImg = `C:\Program Files\qemu\qemu-img.exe`
			}
		}
	}
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", "-b", qm.Config.ImagePath, "-B", qm.driveFormat(), qm.overlayPath)
	if err := cmd.Run(); err != nil {
		// QEMU <= 10.0 used -F for the backing format; QEMU 10.1+ uses -B.
		cmd = exec.Command(qemuImg, "create", "-f", "qcow2", "-b", qm.Config.ImagePath, "-F", qm.driveFormat(), qm.overlayPath)
		return cmd.Run()
	}
	return nil
}

func (qm *QEMUManager) driveFormat() string {
	if qm.Config.ImageFormat == "raw" {
		return "raw"
	}
	return "qcow2"
}

func (qm *QEMUManager) findFreePorts() (vncPort, wsPort int, err error) {
	// 5900 is base for VNC display :0
	for p := 5902; p < 6000; p++ {
		if qm.isPortAvailable(p) {
			vncPort = p
			break
		}
	}
	for p := 5702; p < 5800; p++ {
		if qm.isPortAvailable(p) {
			wsPort = p
			break
		}
	}
	if vncPort == 0 || wsPort == 0 {
		return 0, 0, fmt.Errorf("no free ports available")
	}
	return vncPort, wsPort, nil
}

func (qm *QEMUManager) isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func (qm *QEMUManager) findSingleFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

func connectWithRetry(addr string, retries int, delay time.Duration) (net.Conn, error) {
	for i := 0; i < retries; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			return conn, nil
		}
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("connection timed out")
}


