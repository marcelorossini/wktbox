import os
import socket
import urllib.request
from pathlib import Path


expected = os.environ["BOX_MARKER"]

frontend = urllib.request.urlopen(
    "http://frontend:5173/public/index.html", timeout=5
).read().decode()
if "wktbox fixture" not in frontend:
    raise SystemExit(f"unexpected frontend response: {frontend!r}")

backend = urllib.request.urlopen("http://backend:8000", timeout=5).read().decode()
if backend != expected:
    raise SystemExit(f"backend marker {backend!r}, expected {expected!r}")

with socket.create_connection(("database", 5432), timeout=5) as connection:
    database = connection.recv(1024).decode()
if database != expected:
    raise SystemExit(f"database marker {database!r}, expected {expected!r}")

Path("/artifacts").mkdir(parents=True, exist_ok=True)
Path(f"/artifacts/{expected}.txt").write_text("passed\n", encoding="utf-8")
print(f"e2e passed for {expected}")
