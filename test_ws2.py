import json
import time
import websocket
import threading

def on_message(ws, message):
    data = json.loads(message)
    if data.get("type") == "vm_output":
        print(data.get("payload", ""), end="", flush=True)

def on_open(ws):
    print("\n[WS CONNECTED]")
    # send enter
    ws.send(json.dumps({"type": "input", "payload": "\r"}))
    
    def send_keys():
        time.sleep(5)
        print("\n[Sending 'root\\r']")
        ws.send(json.dumps({"type": "input", "payload": "root\r"}))
        time.sleep(2)
        ws.send(json.dumps({"type": "input", "payload": "ls -la\r"}))
    threading.Thread(target=send_keys).start()

def main():
    session_id = "45004cf0-3162-4cce-9a7f-2b07783e40ce"
    ws_url = f"ws://localhost:8080/ws/session/{session_id}"
    ws = websocket.WebSocketApp(ws_url, on_open=on_open, on_message=on_message)
    ws.run_forever()

if __name__ == "__main__":
    main()
