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
    print("\n[WS CONNECTED]")
    
    def send_keys():
        time.sleep(30)
        print("\n[Typing 'r', 'o', 'o', 't', '\\r']")
        for c in "root\r":
            ws.send(json.dumps({"type": "input", "payload": c}))
            time.sleep(0.5)
        
        time.sleep(5)
        print("\n[Typing 'l', 's', '\\r']")
        for c in "ls\r":
            ws.send(json.dumps({"type": "input", "payload": c}))
            time.sleep(0.5)
            
        time.sleep(5)
        sys.exit(0)
        
    threading.Thread(target=send_keys).start()

def main():
    headers = {"X-VMRunner-User": "admin", "X-VMRunner-Role": "admin"}
    resp = requests.post("http://localhost:8080/api/sessions", json={"challenge_id": "chal_alpine_root"}, headers=headers)
    session_id = resp.json().get("session", {}).get("id")
    ws_url = f"ws://localhost:8080/ws/session/{session_id}"
    ws = websocket.WebSocketApp(ws_url, on_open=on_open, on_message=on_message)
    ws.run_forever()

if __name__ == "__main__":
    main()
