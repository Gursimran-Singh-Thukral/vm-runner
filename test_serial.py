import socket
import time

def main():
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        s.connect(('127.0.0.1', 4444))
        print("Connected")
        s.settimeout(1.0)
        
        while True:
            try:
                data = s.recv(1024)
                if not data:
                    print("Connection closed by server")
                    break
                text = data.decode('utf-8', errors='replace')
                print(text, end='', flush=True)
                
                if "login:" in text:
                    print("\n[Found login prompt, sending 'root']")
                    s.sendall(b"root\n")
            except socket.timeout:
                pass
    except Exception as e:
        print(f"Error: {e}")
    finally:
        s.close()

if __name__ == "__main__":
    main()
