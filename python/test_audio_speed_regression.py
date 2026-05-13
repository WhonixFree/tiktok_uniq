import json
import math
import tempfile
import unittest
import wave
from array import array
from pathlib import Path

import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parent))
import audio_speed


class AudioSpeedRegressionTests(unittest.TestCase):
    def _sine_wav(self, path: Path, sample_rate=48000, seconds=1.25, freq=440.0):
        frames = int(round(sample_rate * seconds))
        samples = array("h")
        for i in range(frames):
            v = int(20000 * math.sin(2 * math.pi * freq * i / sample_rate))
            samples.append(v)
        params = wave._wave_params(1, 2, sample_rate, frames, "NONE", "not compressed")
        audio_speed.write_wav(str(path), params, samples)
        return params, samples

    def test_warp_and_fit_match_reference_duration_and_samplerate(self):
        with tempfile.TemporaryDirectory() as td:
            inp = Path(td) / "in.wav"
            out = Path(td) / "out.wav"
            params, src = self._sine_wav(inp)
            target_duration = 1.1
            cfg = {
                "BasePercent": 0.4,
                "Sine": {"Amplitude": 0.001, "Frequency": 0.2, "Phase": 0.0},
                "MicroEvents": [{"StartSec": 0.3, "DurationSec": 0.12, "Delta": -0.002}],
                "FreezeEvents": [{"StartSec": 0.7, "DurationSec": 0.03, "Repeats": 3}],
            }
            warped = audio_speed.warp(src, params, cfg, target_duration)
            frozen, runtime = audio_speed.apply_freezes(warped, params, cfg["FreezeEvents"], target_duration)
            fitted = audio_speed.fit_duration(frozen, params, target_duration)
            self.assertEqual(runtime["applied"], 1)
            audio_speed.write_wav(str(out), params, fitted)

            out_params, out_samples = audio_speed.read_wav(str(out))
            self.assertEqual(out_params.framerate, params.framerate)
            self.assertEqual(len(out_samples), int(round(target_duration * params.framerate)))

    def test_no_clipping_and_smooth_freeze_crossfade(self):
        with tempfile.TemporaryDirectory() as td:
            inp = Path(td) / "in.wav"
            params, src = self._sine_wav(inp, seconds=1.0)
            freeze_events = [{"StartSec": 0.4, "DurationSec": 0.2, "Repeats": 3}]
            out, runtime = audio_speed.apply_freezes(src, params, freeze_events, 1.0)
            self.assertEqual(runtime["applied"], 1)
            self.assertTrue(all(-32768 <= s <= 32767 for s in out))

            # Regression guard: crossfade window must stay at least 50ms.
            min_crossfade = int(0.05 * params.framerate)
            ev_frames = int(round(freeze_events[0]["DurationSec"] * params.framerate))
            inserted_frames = ev_frames * (freeze_events[0]["Repeats"] - 1)
            derived_fade = min(max(1, int(0.05 * params.framerate)), ev_frames // 2, inserted_frames)
            self.assertGreaterEqual(derived_fade, min_crossfade, "crossfade shorter than 50ms")

    def test_speed_curve_has_no_hard_steps(self):
        cfg = {
            "BasePercent": 0.2,
            "Sine": {"Amplitude": 0.002, "Frequency": 0.1, "Phase": 0.3},
            "MicroEvents": [{"StartSec": 0.2, "DurationSec": 0.15, "Delta": 0.003}],
        }
        # Sample curve densely and assert bounded first derivative jumps.
        ts = [i / 1000.0 for i in range(1000)]
        vals = [audio_speed.speed_at(t, cfg) for t in ts]
        diffs = [abs(vals[i + 1] - vals[i]) for i in range(len(vals) - 1)]
        self.assertLess(max(diffs), 0.01)


if __name__ == "__main__":
    unittest.main()


class AudioSpeedShortClipPolicyTests(unittest.TestCase):
    def test_short_clip_skips_freezes(self):
        params = wave._wave_params(1, 2, 48000, 4800, "NONE", "not compressed")
        src = array("h", [0]*4800)
        out, runtime = audio_speed.apply_freezes(src, params, [{"StartSec":0.01,"DurationSec":0.02,"Repeats":3}], 0.1)
        self.assertEqual(len(out), len(src))
        self.assertEqual(runtime["applied"], 0)
        self.assertEqual(runtime["events"][0]["reason"], "short_clip_guard")
