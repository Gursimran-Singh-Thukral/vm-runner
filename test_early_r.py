import requests
import json
import websocket
import threading
import sys
import time

def on_message(ws, message):
    data = json.loads(message)
    if data.get("type") == "vm_output":
        payload = data.get("payload", "")
        print(payload, end="", flush=True)
        if "boot:" in payload:
            print("\n[Sending \\r IMMEDIATELY upon seeing boot:]")
            ws.send(json.dumps({"type": "input", "payload": "\r"}))
            def wait_and_exit():
                time.sleep(20)
                sys.exit(0)
            threading.Thread(target=wait_and_exit).start()

def on_open(ws):
    print("\n[WS CONNECTED]")

def main():
    headers = {"X-VMRunner-User": "admin", "X-VMRunner-Role": "admin"}
    resp = requests.post("http://localhost:8080/api/sessions", json={"challenge_id": "chal_alpine_root"}, headers=headers)
    session_id = resp.json().get("session", {}).get("id")
    ws_url = f"ws://localhost:8080/ws/session/{session_id}"
    ws = websocket.WebSocketApp(ws_url, on_open=on_open, on_message=on_message)
    ws.run_forever()

if __name__ == "__main__":
    main()
