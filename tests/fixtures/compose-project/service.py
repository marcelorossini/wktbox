import base64
import hashlib
import http.server
import os
import socket
import struct
import sys
from pathlib import Path


def run_http(port: int) -> None:
    marker = os.environ["BOX_MARKER"].encode()

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            self.send_response(200)
            self.end_headers()
            self.wfile.write(marker)

        def log_message(self, format: str, *args: object) -> None:
            return

    http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()


def run_web(port: int) -> None:
    class Handler(http.server.SimpleHTTPRequestHandler):
        def do_GET(self) -> None:
            if self.path == "/ws":
                self.handle_websocket()
                return
            super().do_GET()

        def handle_websocket(self) -> None:
            key = self.headers.get("Sec-WebSocket-Key", "")
            if self.headers.get("Upgrade", "").lower() != "websocket" or not key:
                self.send_error(400, "WebSocket upgrade required")
                return
            accept = base64.b64encode(
                hashlib.sha1(
                    (key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()
                ).digest()
            ).decode()
            self.send_response(101, "Switching Protocols")
            self.send_header("Upgrade", "websocket")
            self.send_header("Connection", "Upgrade")
            self.send_header("Sec-WebSocket-Accept", accept)
            self.end_headers()

            first, second = read_exact(self.connection, 2)
            opcode = first & 0x0F
            masked = second & 0x80
            length = second & 0x7F
            if opcode != 1 or not masked:
                return
            if length == 126:
                length = struct.unpack("!H", read_exact(self.connection, 2))[0]
            elif length == 127:
                length = struct.unpack("!Q", read_exact(self.connection, 8))[0]
            mask = read_exact(self.connection, 4)
            encoded = read_exact(self.connection, length)
            payload = bytes(value ^ mask[index % 4] for index, value in enumerate(encoded))
            header = bytes([0x81])
            if length < 126:
                header += bytes([length])
            elif length < 65536:
                header += bytes([126]) + struct.pack("!H", length)
            else:
                header += bytes([127]) + struct.pack("!Q", length)
            self.connection.sendall(header + payload)

        def log_message(self, format: str, *args: object) -> None:
            return

    http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()


def read_exact(connection: socket.socket, size: int) -> bytes:
    result = bytearray()
    while len(result) < size:
        block = connection.recv(size - len(result))
        if not block:
            raise ConnectionError("unexpected end of WebSocket frame")
        result.extend(block)
    return bytes(result)


def run_tcp(port: int, marker_path: Path) -> None:
    if not marker_path.exists():
        marker_path.write_text(os.environ["BOX_MARKER"], encoding="utf-8")

    with socket.socket() as server:
        server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        server.bind(("0.0.0.0", port))
        server.listen()
        while True:
            connection, _ = server.accept()
            with connection:
                connection.sendall(marker_path.read_bytes())


if __name__ == "__main__":
    mode = sys.argv[1]
    if mode == "http":
        run_http(int(sys.argv[2]))
    elif mode == "web":
        run_web(int(sys.argv[2]))
    elif mode == "tcp":
        run_tcp(int(sys.argv[2]), Path(sys.argv[3]))
    else:
        raise SystemExit(f"unknown mode: {mode}")
