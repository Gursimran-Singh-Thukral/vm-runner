package main
import ("fmt"; "log"; "github.com/gorilla/websocket")
func main() {
    c, _, err := websocket.DefaultDialer.Dial("ws://localhost:5000/ws/session/616e78a4-2c18-46b8-a7d7-89b95f2ee8d7", nil)
    if err != nil { log.Fatal("dial:", err) }
    defer c.Close()
    fmt.Println("Connected!")
    _, message, err := c.ReadMessage()
    if err != nil { log.Fatal("read:", err) }
    fmt.Printf("recv: %s\n", message)
}
