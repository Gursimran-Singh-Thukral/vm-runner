import requests
import json
import time
import websocket # pip install websocket-client
import threading

def on_message(ws, message):
    data = json.loads(message)
    if data.get("type") == "vm_output":
        print(data.get("payload", ""), end="", flush=True)

def on_error(ws, error):
    print(f"\n[WS ERROR] {error}")

def on_close(ws, close_status_code, close_msg):
    print("\n[WS CLOSED]")

def on_open(ws):
    print("\n[WS CONNECTED]")
    # send enter
    ws.send(json.dumps({"type": "input", "payload": "\r"}))
    
    def send_keys():
        time.sleep(5)
        print("\n[Sending 'root\\r']")
        ws.send(json.dumps({"type": "input", "payload": "root\r"}))
    threading.Thread(target=send_keys).start()

def main():
    # 1. Create Session
    try:
        resp = requests.post("http://localhost:8080/api/sessions", json={"challenge_id": "chal_alpine_root"})
        resp.raise_for_status()
        session_id = resp.json().get("session", {}).get("id")
        if not session_id:
            print("No session ID returned")
            return
        print(f"Created session: {session_id}")
    except Exception as e:
        print(f"Failed to create session: {e}")
        return

    # 2. Connect WebSocket
    ws_url = f"ws://localhost:8080/ws/session/{session_id}"
    ws = websocket.WebSocketApp(ws_url,
                                on_open=on_open,
                                on_message=on_message,
                                on_error=on_error,
                                on_close=on_close)
    ws.run_forever()

if __name__ == "__main__":
    main()
