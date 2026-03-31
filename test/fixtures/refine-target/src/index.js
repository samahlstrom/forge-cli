import http from "node:http";

const PORT = process.env.PORT || 3999;

const routes = {
  "/health": (_req, res) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ status: "ok", uptime: process.uptime() }));
  },
};

const server = http.createServer((req, res) => {
  const handler = routes[req.url];
  if (handler) return handler(req, res);
  res.writeHead(404);
  res.end("not found");
});

server.listen(PORT, () => console.log(`listening on ${PORT}`));
export default server;
