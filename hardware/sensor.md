# sensor

## Chosen

ICM-20948 on the SparkFun 9DoF Qwiic breakout, accelerometer only (gyro and magnetometer off).

TODO (Twyla): why it was chosen over LIS3DH / ADXL345 / LIS2DH12, in "chose X over Y because Z" form.

## Settings (`components/sensing/accel.c`)

| Setting | Value | Why |
|---|---|---|
| Range | ±2 g | Contract B1 |
| Sample rate | 1125 / 11 ≈ 102.3 Hz | Nearest the chip gets to 100 Hz; a 500-sample window lasts ~4.9 s, not 5 s (open) |
| Low-pass filter | DLPFCFG=3, ~50 Hz | Anti-aliasing at ~100 Hz sampling, and a lower noise floor |
| Readout | On-chip FIFO | The sensor's clock paces samples, so FreeRTOS timing jitter doesn't matter |

## Bench results (Oct 22, 2026, firmware bench test, board lying flat)

| Test | `vibration_rms` |
|---|---|
| Still on a desk | 0.0014-0.0015 g (noise floor) |
| Single tap on the desk | ~0.004 g for one window |
| Taped to a speaker playing 20 Hz | ~0.027 g, every window while playing |

Speaker on is ~18× the still floor, and both on-periods and all off-periods repeated to within a few percent.
A brief tap barely moves one window: RMS averages over the whole window, so short bumps are diluted.

Raw axes when flat and still: x ≈ +0.01, y ≈ -0.01, z ≈ +1.02 g (offsets of 1-2 % are normal).
