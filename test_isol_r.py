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
    def run_test():
        time.sleep(5)
        print("\n[Sending \\r at boot prompt]")
        ws.send(json.dumps({"type": "input", "payload": "\r"}))
        time.sleep(30)
        sys.exit(0)
    threading.Thread(target=run_test).start()

def main():
    headers = {"X-VMRunner-User": "admin", "X-VMRunner-Role": "admin"}
    resp = requests.post("http://localhost:8080/api/sessions", json={"challenge_id": "chal_alpine_root"}, headers=headers)
    session_id = resp.json().get("session", {}).get("id")
    ws_url = f"ws://localhost:8080/ws/session/{session_id}"
    ws = websocket.WebSocketApp(ws_url, on_open=on_open, on_message=on_message)
    ws.run_forever()

if __name__ == "__main__":
    main()
