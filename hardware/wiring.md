# wiring

ESP32-C6 to accelerometer wiring (I2C pins, power, pull-ups).

## Parts

- MCU: ESP32-C6 dev board
- Accelerometer: SparkFun 9DoF IMU Breakout, ICM-20948 (Qwiic)

## Connections

| ESP32-C6 | ICM-20948 breakout | Qwiic wire | Notes |
|---|---|---|---|
| 3V3 | 3V3 | red | Never 5 V. The board regulates down to the chip's 1.8 V. |
| GND | GND | black | |
| GPIO 6 | SDA | blue | I2C data |
| GPIO 7 | SCL | green | I2C clock |

Not connected: AD0, INT, FSYNC, and the SPI pins (CS, SDO/MISO, etc.).

## I2C settings

- Address: `0x69` (default; bridging the ADR jumper changes it to `0x68`)
- Pull-ups: on the breakout (I2C jumper, enabled by default); ESP32 internal pull-ups off
- Bus speed: 100 kHz (400 kHz timed out on the bench setup)
- Firmware config: `CONFIG_SPINCYCLE_I2C_SDA_GPIO=6`, `CONFIG_SPINCYCLE_I2C_SCL_GPIO=7`,
  `CONFIG_SPINCYCLE_ACCEL_I2C_ADDR=0x69` (`components/sensing/Kconfig.projbuild`).
  Change them there and here together.

## Pins to avoid on the ESP32-C6

| GPIO | Reason |
|---|---|
| 4, 5, 8, 9, 15 | Strapping pins (boot mode); 8 is also the DevKitC RGB LED |
| 12, 13 | USB D-/D+ (flashing and serial log) |
| 16, 17 | UART0 console |
| 24-30 | Internal SPI flash |

## Troubleshooting

- **"no ACK at 0x69" and the scan times out at every address, but the line check reports pull-ups:**
  the SDA/SCL wires reach the breakout (its pull-ups are what the check sees), but the signals don't
  reach the chip. Seen Oct 21-22, 2026; a power-cycle alone didn't fix it, and it worked again after
  the wiring was handled. TODO (Aly): note exactly what was reseated or moved.
  Order to check: reseat the Qwiic plug at both ends, confirm SDA/SCL by the board labels, swap cable.
- **Serial logs:** use the board's **UART** USB-C socket (`/dev/cu.usbserial-*`, CP2102N) for recordings.
  The **USB** socket's built-in console (`/dev/cu.usbmodem*`) dropped ~2 minutes of log lines when
  the host didn't keep up.

## Later

- INT to a free GPIO (e.g. GPIO 2 or 3) if the firmware moves to FIFO watermark interrupts.
- Keep the cable short (< 20 cm) and strain-relieved; flexing wires on a vibrating machine cause intermittent I2C errors.
