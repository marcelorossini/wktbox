# Emulates Docker Desktop's host.docker.internal forwarding: accepts
# connections on the address containers resolve host.docker.internal to and
# relays them to the same port on host loopback.
# Usage: python3 -I desktop_forwarder.py <listen-ip> <port>
import asyncio
import sys


async def pipe(reader, writer):
    try:
        while data := await reader.read(65536):
            writer.write(data)
            await writer.drain()
    finally:
        writer.close()


async def handle(client_reader, client_writer):
    port = int(sys.argv[2])
    upstream_reader, upstream_writer = await asyncio.open_connection("127.0.0.1", port)
    await asyncio.gather(
        pipe(client_reader, upstream_writer),
        pipe(upstream_reader, client_writer),
        return_exceptions=True,
    )


async def main():
    server = await asyncio.start_server(handle, sys.argv[1], int(sys.argv[2]))
    print("forwarding", sys.argv[1], sys.argv[2], flush=True)
    async with server:
        await server.serve_forever()


asyncio.run(main())
