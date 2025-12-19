package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/websocket"
)

func echoHandler(ws *websocket.Conn) {
	log.Printf("WebSocket connection established from %s", ws.RemoteAddr())
	defer func() {
		log.Printf("WebSocket connection closed from %s", ws.RemoteAddr())
		ws.Close()
	}()

	for {
		var msg string
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			log.Printf("Error receiving message: %v", err)
			return
		}
		log.Printf("Received: %s", msg)

		reply := fmt.Sprintf("echo: %s", msg)
		if err := websocket.Message.Send(ws, reply); err != nil {
			log.Printf("Error sending message: %v", err)
			return
		}
		log.Printf("Sent: %s", reply)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	http.Handle("/ws", websocket.Handler(echoHandler))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>WebSocket Echo Test</title></head>
<body>
<h1>WebSocket Echo Test</h1>
<div>
  <input type="text" id="msg" value="hello" />
  <button onclick="send()">Send</button>
  <button onclick="connect()">Connect</button>
  <button onclick="disconnect()">Disconnect</button>
</div>
<pre id="log" style="background:#eee;padding:10px;margin-top:10px;height:300px;overflow:auto;"></pre>
<script>
let ws;
function log(msg) {
  document.getElementById('log').textContent += new Date().toISOString() + ' ' + msg + '\n';
}
function connect() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = proto + '//' + location.host + '/ws';
  log('Connecting to ' + url);
  ws = new WebSocket(url);
  ws.onopen = () => log('OPEN');
  ws.onclose = (e) => log('CLOSE code=' + e.code + ' reason=' + e.reason);
  ws.onerror = (e) => log('ERROR ' + e);
  ws.onmessage = (e) => log('RECV: ' + e.data);
}
function send() {
  const msg = document.getElementById('msg').value;
  log('SEND: ' + msg);
  ws.send(msg);
}
function disconnect() {
  if (ws) ws.close();
}
connect();
</script>
</body>
</html>`)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK\n")
	})

	log.Printf("WebSocket echo server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("Error starting server: %v", err)
		os.Exit(1)
	}
}
