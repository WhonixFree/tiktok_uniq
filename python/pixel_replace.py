#!/usr/bin/env python3
import argparse
import hashlib
import random


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument("--input", required=True)
    p.add_argument("--output", required=True)
    p.add_argument("--width", type=int, required=True)
    p.add_argument("--height", type=int, required=True)
    p.add_argument("--percent", type=float, required=True)
    p.add_argument("--area-x", type=int, required=True)
    p.add_argument("--area-y", type=int, required=True)
    p.add_argument("--area-w", type=int, required=True)
    p.add_argument("--area-h", type=int, required=True)
    p.add_argument("--neighbor-offset", type=int, required=True)
    p.add_argument("--seed", type=int, required=True)
    return p.parse_args()


def idx(x, y, w):
    return (y * w + x) * 3


def main():
    a = parse_args()
    frame_size = a.width * a.height * 3
    percent = max(0.0, min(100.0, a.percent))
    n_off = a.neighbor_offset or 1

    ax = max(0, min(a.width - 1, a.area_x))
    ay = max(0, min(a.height - 1, a.area_y))
    aw = max(1, min(a.width - ax, a.area_w))
    ah = max(1, min(a.height - ay, a.area_h))

    with open(a.input, "rb") as fin, open(a.output, "wb") as fout:
        frame_idx = 0
        while True:
            raw = fin.read(frame_size)
            if not raw:
                break
            if len(raw) != frame_size:
                break

            frame = bytearray(raw)
            total = aw * ah
            replace_count = int(round(total * (percent / 100.0)))
            if replace_count > 0:
                seed_bytes = hashlib.sha256(f"{a.seed}:{frame_idx}".encode()).digest()[:8]
                rng = random.Random(int.from_bytes(seed_bytes, "little"))
                picks = rng.sample(range(total), min(replace_count, total))
                for flat in picks:
                    y = flat // aw
                    x = flat % aw
                    choices = [
                        (x + n_off, y), (x - n_off, y), (x, y + n_off), (x, y - n_off),
                        (x + n_off, y + n_off), (x + n_off, y - n_off),
                        (x - n_off, y + n_off), (x - n_off, y - n_off),
                    ]
                    nx, ny = choices[rng.randrange(len(choices))]
                    nx = max(0, min(aw - 1, nx))
                    ny = max(0, min(ah - 1, ny))
                    gx, gy = ax + x, ay + y
                    gnx, gny = ax + nx, ay + ny
                    dst = idx(gx, gy, a.width)
                    src = idx(gnx, gny, a.width)
                    frame[dst:dst+3] = frame[src:src+3]

            fout.write(frame)
            frame_idx += 1


if __name__ == "__main__":
    main()
