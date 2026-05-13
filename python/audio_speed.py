#!/usr/bin/env python3
"""Deterministic WAV temporal speed processor for videobatch.

Applies base speed, slow sine, short smoothed micro perturbations, and tiny
crossfaded freeze repeats without FFmpeg atempo. Input and output are PCM WAV;
the sample rate and channel count are preserved.
"""

import argparse
import json
import math
import wave
from array import array

MIN_SPEED = 0.95
MAX_SPEED = 1.05


def clamp(value, lo, hi):
    return max(lo, min(hi, value))


def read_wav(path):
    with wave.open(path, "rb") as wav:
        params = wav.getparams()
        if params.sampwidth != 2:
            raise ValueError("audio_speed.py expects 16-bit PCM WAV input")
        frames = wav.readframes(params.nframes)
    samples = array("h")
    samples.frombytes(frames)
    return params, samples


def write_wav(path, params, samples):
    with wave.open(path, "wb") as wav:
        wav.setnchannels(params.nchannels)
        wav.setsampwidth(params.sampwidth)
        wav.setframerate(params.framerate)
        wav.writeframes(samples.tobytes())


def speed_at(t, cfg):
    base = 1.0 + float(cfg.get("BasePercent", 0.0)) / 100.0
    sine = cfg.get("Sine", {}) or {}
    amp = float(sine.get("Amplitude", 0.0))
    freq = float(sine.get("Frequency", 0.0))
    phase = float(sine.get("Phase", 0.0))
    value = base * (1.0 + amp * math.sin(2.0 * math.pi * freq * t + phase))
    for ev in cfg.get("MicroEvents", []) or []:
        start = float(ev.get("StartSec", 0.0))
        dur = max(0.0, float(ev.get("DurationSec", 0.0)))
        if dur <= 0 or t < start or t > start + dur:
            continue
        x = (t - start) / dur
        # Raised-sine window: smooth attack and release, no speed step clicks.
        window = math.sin(math.pi * x) ** 2
        value += float(ev.get("Delta", 0.0)) * window
    return clamp(value, MIN_SPEED, MAX_SPEED)


def interpolate(samples, channels, pos):
    frame_count = len(samples) // channels
    if frame_count == 0:
        return [0] * channels
    pos = clamp(pos, 0.0, max(0.0, frame_count - 1.0))
    left = int(math.floor(pos))
    right = min(left + 1, frame_count - 1)
    frac = pos - left
    out = []
    for ch in range(channels):
        a = samples[left * channels + ch]
        b = samples[right * channels + ch]
        out.append(int(round(a + (b - a) * frac)))
    return out


def warp(samples, params, cfg, target_duration):
    channels = params.nchannels
    sample_rate = params.framerate
    target_frames = max(1, int(round(target_duration * sample_rate)))
    input_pos = 0.0
    out = array("h")
    for _ in range(target_frames):
        t = input_pos / sample_rate
        out.extend(interpolate(samples, channels, input_pos))
        input_pos += speed_at(t, cfg)
    return out


def apply_freezes(samples, params, freeze_events):
    if not freeze_events:
        return samples
    channels = params.nchannels
    sample_rate = params.framerate
    out = array("h", samples)
    offset_frames = 0
    for ev in sorted(freeze_events, key=lambda item: float(item.get("StartSec", 0.0))):
        start = int(round(float(ev.get("StartSec", 0.0)) * sample_rate)) + offset_frames
        length = int(round(float(ev.get("DurationSec", 0.0)) * sample_rate))
        repeats = int(ev.get("Repeats", 2))
        if length <= 0 or repeats < 2:
            continue
        frame_count = len(out) // channels
        if start < 0 or start + length >= frame_count:
            continue
        start_i = start * channels
        end_i = (start + length) * channels
        segment = array("h", out[start_i:end_i])
        insert = array("h")
        for _ in range(repeats - 1):
            insert.extend(segment)
        fade_frames = min(max(1, int(0.05 * sample_rate)), length // 2, len(insert) // max(1, channels))
        # Crossfade inserted edges against neighbors to keep transitions smooth.
        for i in range(fade_frames):
            gain_in = (i + 1) / fade_frames
            gain_out = 1.0 - gain_in
            for ch in range(channels):
                idx = i * channels + ch
                before = out[max(0, start_i - fade_frames * channels) + idx] if start_i >= fade_frames * channels else insert[idx]
                insert[idx] = int(round(before * gain_out + insert[idx] * gain_in))
                tail_idx = len(insert) - (i + 1) * channels + ch
                after_idx = min(len(out) - channels + ch, end_i + (fade_frames - i - 1) * channels + ch)
                insert[tail_idx] = int(round(out[after_idx] * gain_out + insert[tail_idx] * gain_in))
        out[start_i:start_i] = insert
        offset_frames += len(insert) // channels
    return out


def fit_duration(samples, params, target_duration):
    channels = params.nchannels
    sample_rate = params.framerate
    target_frames = max(1, int(round(target_duration * sample_rate)))
    frames = len(samples) // channels
    if frames == target_frames:
        return samples
    fitted = array("h")
    if target_frames == 1:
        fitted.extend(interpolate(samples, channels, 0.0))
        return fitted
    scale = max(1, frames - 1) / max(1, target_frames - 1)
    for i in range(target_frames):
        fitted.extend(interpolate(samples, channels, i * scale))
    return fitted


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--recipe", required=True)
    parser.add_argument("--target-duration", type=float, required=True)
    args = parser.parse_args()

    with open(args.recipe, "r", encoding="utf-8") as fh:
        recipe = json.load(fh)
    audio_cfg = recipe.get("AudioSpeed", {}) or {}
    params, samples = read_wav(args.input)
    warped = warp(samples, params, audio_cfg, args.target_duration)
    frozen = apply_freezes(warped, params, audio_cfg.get("FreezeEvents", []) or [])
    fitted = fit_duration(frozen, params, args.target_duration)
    write_wav(args.output, params, fitted)


if __name__ == "__main__":
    main()
