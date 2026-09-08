import requests
import json
import time
import websocket
import threading
import sys

def on_message(ws, message):
    data = json.loads(message)
    if data.get("type") == "vm_output":
        print(data.get("payload", ""), end="", flush=True)

def on_open(ws):
    print("\n[WS CONNECTED] Waiting 30s for boot...")
    
    def send_keys():
        time.sleep(30)
        print("\n[Sending 'root\\r']")
        ws.send(json.dumps({"type": "input", "payload": "root\r"}))
        time.sleep(2)
        print("\n[Sending 'ls -la\\r']")
        ws.send(json.dumps({"type": "input", "payload": "ls -la\r"}))
        time.sleep(5)
        print("\n[Exiting]")
        sys.exit(0)
        
    threading.Thread(target=send_keys).start()

def main():
    print("Creating session...")
    headers = {"X-VMRunner-User": "admin", "X-VMRunner-Role": "admin"}
    resp = requests.post("http://localhost:8080/api/sessions", json={"challenge_id": "chal_alpine_root"}, headers=headers)
    session_id = resp.json().get("session", {}).get("id")
    print(f"Session: {session_id}")
    
    ws_url = f"ws://localhost:8080/ws/session/{session_id}"
    ws = websocket.WebSocketApp(ws_url, on_open=on_open, on_message=on_message)
    ws.run_forever()

if __name__ == "__main__":
    main()
