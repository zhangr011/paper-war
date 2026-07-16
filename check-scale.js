const WebSocket = require("ws");
const ws = new WebSocket("ws://localhost:9091/ws");
ws.on("open", () => {
  ws.send(JSON.stringify({ type: "login", name: "scale-check", token: "test-scale" }));
});
ws.on("message", (data, isBinary) => {
  if (isBinary) {
    if (data[0] === 0xFF && data[1] === 0xFD) {
      console.log("Map frame bytes (excl prefix):", data.length - 2);
      console.log("Expected for 30x48:", 30 * 48);
      console.log("Old 48x96 would be:", 48 * 96);
      setTimeout(() => { ws.close(); process.exit(0); }, 300);
    }
    return;
  }
  const msg = JSON.parse(data.toString());
  if (msg.type === "login_ok") {
    console.log("login_ok player_id:", msg.player_id);
    ws.send(JSON.stringify({ type: "start_solo" }));
  }
  if (msg.type === "match_found") {
    console.log("match_found: map_w=" + msg.map_w + " map_h=" + msg.map_h);
  }
});
setTimeout(() => { console.log("timeout"); process.exit(1); }, 5000);
