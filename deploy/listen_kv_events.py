#!/usr/bin/env python3
import argparse
import struct
import sys
import time

import msgpack
import zmq


def as_u64(v):
    if isinstance(v, int):
        return v & ((1 << 64) - 1)
    if isinstance(v, (bytes, bytearray)):
        return int.from_bytes(v[-8:], "big", signed=False)
    return v


def summarize_event(event):
    if not isinstance(event, list) or not event:
        return "unknown"

    kind = event[0]
    if kind == "BlockStored":
        hashes = event[1] if len(event) > 1 and isinstance(event[1], list) else []
        parent = as_u64(event[2]) if len(event) > 2 else None
        tokens = event[3] if len(event) > 3 and isinstance(event[3], list) else []
        block_size = event[4] if len(event) > 4 else None
        medium = event[6] if len(event) > 6 else None
        return (
            f"BlockStored hashes={len(hashes)} parent={parent} "
            f"tokens={len(tokens)} block_size={block_size} medium={medium}"
        )
    if kind == "BlockRemoved":
        hashes = event[1] if len(event) > 1 and isinstance(event[1], list) else []
        medium = event[2] if len(event) > 2 else None
        return f"BlockRemoved hashes={len(hashes)} medium={medium}"
    if kind == "AllBlocksCleared":
        return "AllBlocksCleared"
    return str(kind)


def main():
    parser = argparse.ArgumentParser(description="Listen to vLLM/SGLang KV cache ZMQ events.")
    parser.add_argument("endpoint", nargs="?", default="tcp://127.0.0.1:5557")
    parser.add_argument("--topic", default="kv-events", help="ZMQ topic filter; use '' for all topics")
    parser.add_argument("--count", type=int, default=5, help="stop after N batches; 0 means unlimited")
    parser.add_argument("--timeout", type=float, default=60, help="seconds to wait before exiting")
    args = parser.parse_args()

    ctx = zmq.Context.instance()
    sock = ctx.socket(zmq.SUB)
    sock.setsockopt(zmq.SUBSCRIBE, args.topic.encode())
    sock.setsockopt(zmq.RCVTIMEO, 1000)
    sock.connect(args.endpoint)

    deadline = time.monotonic() + args.timeout
    seen = 0
    print(f"listening endpoint={args.endpoint} topic={args.topic!r}", flush=True)

    while args.timeout <= 0 or time.monotonic() < deadline:
        try:
            frames = sock.recv_multipart()
        except zmq.Again:
            continue

        if len(frames) != 3:
            print(f"skip: expected 3 frames, got {len(frames)}", file=sys.stderr)
            continue

        topic = frames[0].decode(errors="replace")
        seq = struct.unpack(">Q", frames[1])[0]
        payload = msgpack.unpackb(frames[2], raw=False, strict_map_key=False)
        batch_ts = payload[0] if len(payload) > 0 else None
        events = payload[1] if len(payload) > 1 and isinstance(payload[1], list) else []
        dp_rank = payload[2] if len(payload) > 2 else None

        seen += 1
        print(f"\n#{seen} topic={topic} seq={seq} ts={batch_ts} dp_rank={dp_rank} events={len(events)}")
        for event in events[:10]:
            print(f"  - {summarize_event(event)}")
        if len(events) > 10:
            print(f"  ... {len(events) - 10} more")

        if args.count and seen >= args.count:
            return 0

    print(f"timeout: no more events after {args.timeout}s, seen={seen}", file=sys.stderr)
    return 1 if seen == 0 else 0


if __name__ == "__main__":
    raise SystemExit(main())
