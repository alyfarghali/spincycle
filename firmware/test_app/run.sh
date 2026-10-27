#!/bin/sh
set -e
port=${1:?usage: firmware/test_app/run.sh /dev/cu.usbserial-XXXX}
cd "$(dirname "$0")"
idf.py build > /dev/null
idf.py -p "$port" flash > /dev/null
python - "$port" <<'PY'
import re, serial, sys, time
port = sys.argv[1]
for _ in range(50):  # the port disappears for a moment while the board resets after flashing
    try:
        s = serial.Serial(port, 115200, timeout=1)
        break
    except serial.SerialException:
        time.sleep(0.2)
else:
    sys.exit(f"can't open {port}")
deadline = time.time() + 180
while time.time() < deadline:
    line = s.readline().decode(errors="replace").rstrip()
    if re.search(r":(PASS|FAIL|IGNORE)", line):
        print(line.split("/")[-1])
    m = re.match(r"(\d+) Tests (\d+) Failures (\d+) Ignored", line)
    if m:
        print(line)
        sys.exit(1 if int(m.group(2)) else 0)
sys.exit("timed out waiting for the Unity summary")
PY
