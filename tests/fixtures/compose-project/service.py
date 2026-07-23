import http.server
import os
import socket
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
    elif mode == "tcp":
        run_tcp(int(sys.argv[2]), Path(sys.argv[3]))
    else:
        raise SystemExit(f"unknown mode: {mode}")
