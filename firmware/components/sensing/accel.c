// Accelerometer driver: I2C setup and reading x, y, z samples at 100 Hz.
// Chip: ICM-20948 (SparkFun breakout). Gyro and magnetometer are left off.
#include "accel.h"

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "esp_check.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "sdkconfig.h"

#define I2C_TIMEOUT_MS 20

// Registers. The ICM-20948 has 4 register banks; REG_BANK_SEL exists in all of them.
#define REG_BANK_SEL        0x7F
#define BANK0               0x00
#define BANK2               0x20
// bank 0
#define B0_WHO_AM_I         0x00  // reads 0xEA
#define B0_PWR_MGMT_1       0x06
#define B0_PWR_MGMT_2       0x07
#define B0_ACCEL_XOUT_H     0x2D  // XH XL YH YL ZH ZL, big-endian int16
#define B0_USER_CTRL        0x03  // bit 6 FIFO_EN
#define B0_INT_STATUS_2     0x1B  // bits 4:0 FIFO overflow, cleared on read
#define B0_FIFO_EN_1        0x66  // external I2C slaves into FIFO (keep 0)
#define B0_FIFO_EN_2        0x67  // bit 4 ACCEL_FIFO_EN
#define B0_FIFO_RST         0x68  // write 0x1F then 0x00
#define B0_FIFO_MODE        0x69  // 0 = stream
#define B0_FIFO_COUNTH      0x70  // 13-bit byte count, COUNTH bits 4:0 then COUNTL
#define B0_FIFO_R_W         0x72
#define FIFO_FRAME_BYTES    6     // accel only: XH XL YH YL ZH ZL
#define FIFO_READ_MAX       64    // frames per accel_fifo_read call
// bank 2
#define B2_ACCEL_SMPLRT_DIV_1 0x10
#define B2_ACCEL_SMPLRT_DIV_2 0x11
#define B2_ACCEL_CONFIG     0x14

#define WHO_AM_I_VALUE      0xEA
#define LSB_PER_G_2G        16384.0f  // +-2 g full scale

static const char *TAG = "accel";
static i2c_master_dev_handle_t s_dev;

static esp_err_t write_reg(uint8_t reg, uint8_t val)
{
    uint8_t buf[2] = { reg, val };
    return i2c_master_transmit(s_dev, buf, sizeof(buf), I2C_TIMEOUT_MS);
}

static esp_err_t read_regs(uint8_t reg, uint8_t *out, size_t n)
{
    return i2c_master_transmit_receive(s_dev, &reg, 1, out, n, I2C_TIMEOUT_MS);
}

// Debug aid: read one line as a plain input, before the I2C peripheral owns it (after a failed
// transfer the peripheral itself may hold a line low, which made earlier readings misleading).
//   no-pull 1             -> breakout pull-up is there: connected and powered (good)
//   no-pull 0, pull-up 1  -> nothing pulling it high: not connected, or breakout pull-up missing
//   no-pull 0, pull-up 0  -> something drives it to GND (wire on GND, short)
static void check_line(const char *name, int gpio)
{
    gpio_reset_pin(gpio);
    gpio_set_direction(gpio, GPIO_MODE_INPUT);
    gpio_set_pull_mode(gpio, GPIO_FLOATING);
    vTaskDelay(pdMS_TO_TICKS(5));
    int floating = gpio_get_level(gpio);
    gpio_set_pull_mode(gpio, GPIO_PULLUP_ONLY);
    vTaskDelay(pdMS_TO_TICKS(5));
    int pulled = gpio_get_level(gpio);
    gpio_reset_pin(gpio);
    ESP_LOGI(TAG, "line check %s (GPIO %d): no-pull=%d internal-pull-up=%d -> %s", name, gpio, floating, pulled,
             floating ? "OK, pulled up by breakout"
             : pulled ? "NOT CONNECTED (nothing pulls it high)"
                      : "SHORTED TO GND");
}

// Debug aid for "no ACK": an address scan narrows it to wiring vs address.
static void diagnose_bus(i2c_master_bus_handle_t bus)
{
    int found = 0;
    for (uint16_t addr = 0x08; addr < 0x78; addr++) {
        if (i2c_master_probe(bus, addr, 20) == ESP_OK) {
            ESP_LOGE(TAG, "scan: device answers at 0x%02X", addr);
            found++;
        }
    }
    if (found == 0) {
        ESP_LOGE(TAG, "scan: nothing on the bus (power, GND, or SDA/SCL on the wrong pins)");
    }
}

esp_err_t accel_init(int sda_gpio, int scl_gpio)
{
    check_line("SDA", sda_gpio);
    check_line("SCL", scl_gpio);

    i2c_master_bus_config_t bus_cfg = {
        .i2c_port = -1,  // auto-select
        .sda_io_num = sda_gpio,
        .scl_io_num = scl_gpio,
        .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7,
        .flags.enable_internal_pullup = false,  // breakout has its own pull-ups
    };
    i2c_master_bus_handle_t bus;
    ESP_RETURN_ON_ERROR(i2c_new_master_bus(&bus_cfg, &bus), TAG, "i2c bus");

    i2c_device_config_t dev_cfg = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = CONFIG_SPINCYCLE_ACCEL_I2C_ADDR,
        .scl_speed_hz = 100000,  // 400 kHz timed out through the breakout's level shifters + jumpers
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(bus, &dev_cfg, &s_dev), TAG, "i2c device");

    // 1. Is the chip there and is it the right one?
    uint8_t who = 0;
    if (write_reg(REG_BANK_SEL, BANK0) != ESP_OK) {
        ESP_LOGE(TAG, "no ACK at 0x%02X: check wiring/address", CONFIG_SPINCYCLE_ACCEL_I2C_ADDR);
        diagnose_bus(bus);
        return ESP_ERR_NOT_FOUND;
    }
    ESP_RETURN_ON_ERROR(read_regs(B0_WHO_AM_I, &who, 1), TAG, "WHO_AM_I");
    if (who != WHO_AM_I_VALUE) {
        ESP_LOGE(TAG, "WHO_AM_I = 0x%02X, expected 0x%02X", who, WHO_AM_I_VALUE);
        return ESP_ERR_NOT_FOUND;
    }
    ESP_LOGI(TAG, "ICM-20948 found at 0x%02X (SDA %d, SCL %d)", CONFIG_SPINCYCLE_ACCEL_I2C_ADDR, sda_gpio, scl_gpio);

    // 2. Reset, then wake. The chip powers up asleep and reads zeros until woken.
    ESP_RETURN_ON_ERROR(write_reg(B0_PWR_MGMT_1, 0x80), TAG, "reset");
    vTaskDelay(pdMS_TO_TICKS(100));
    ESP_RETURN_ON_ERROR(write_reg(REG_BANK_SEL, BANK0), TAG, "bank select");
    ESP_RETURN_ON_ERROR(write_reg(B0_PWR_MGMT_1, 0x01), TAG, "wake, auto clock");
    ESP_RETURN_ON_ERROR(write_reg(B0_PWR_MGMT_2, 0x07), TAG, "accel on, gyro off");
    vTaskDelay(pdMS_TO_TICKS(20));

    // 3. Accel config (bank 2).
    //    ODR = 1125 / (1 + div) Hz; div 10 -> 102.3 Hz, the closest to 100 Hz.
    //    ACCEL_CONFIG: DLPFCFG=3 (~50 Hz bandwidth, anti-aliasing for ~100 Hz sampling),
    //    FS_SEL=0 (+-2 g), FCHOICE=1 (DLPF on) -> (3 << 3) | (0 << 1) | 1 = 0x19.
    ESP_RETURN_ON_ERROR(write_reg(REG_BANK_SEL, BANK2), TAG, "bank select");
    ESP_RETURN_ON_ERROR(write_reg(B2_ACCEL_SMPLRT_DIV_1, 0x00), TAG, "smplrt div 1");
    ESP_RETURN_ON_ERROR(write_reg(B2_ACCEL_SMPLRT_DIV_2, 10), TAG, "smplrt div 2");
    ESP_RETURN_ON_ERROR(write_reg(B2_ACCEL_CONFIG, 0x19), TAG, "accel config");
    ESP_RETURN_ON_ERROR(write_reg(REG_BANK_SEL, BANK0), TAG, "bank select");  // everything below is bank 0

    // 4. FIFO: accel only, so every frame is exactly 6 bytes. Gyro/temp/slaves in the
    //    FIFO would change the frame size and misalign every sample after the first.
    ESP_RETURN_ON_ERROR(write_reg(B0_FIFO_EN_1, 0x00), TAG, "fifo en 1");
    ESP_RETURN_ON_ERROR(write_reg(B0_FIFO_EN_2, 0x10), TAG, "fifo en 2 (accel)");
    ESP_RETURN_ON_ERROR(write_reg(B0_FIFO_MODE, 0x00), TAG, "fifo mode stream");
    ESP_RETURN_ON_ERROR(write_reg(B0_USER_CTRL, 0x40), TAG, "fifo enable");
    return accel_fifo_reset();
}

static void decode(const uint8_t *b, accel_sample_t *out)
{
    out->x = (int16_t)((b[0] << 8) | b[1]) / LSB_PER_G_2G;
    out->y = (int16_t)((b[2] << 8) | b[3]) / LSB_PER_G_2G;
    out->z = (int16_t)((b[4] << 8) | b[5]) / LSB_PER_G_2G;
}

esp_err_t accel_read(accel_sample_t *out)
{
    uint8_t b[6];
    esp_err_t err = read_regs(B0_ACCEL_XOUT_H, b, sizeof(b));
    if (err != ESP_OK) {
        return err;
    }
    decode(b, out);
    return ESP_OK;
}

esp_err_t accel_fifo_reset(void)
{
    ESP_RETURN_ON_ERROR(write_reg(B0_FIFO_RST, 0x1F), TAG, "fifo reset");
    ESP_RETURN_ON_ERROR(write_reg(B0_FIFO_RST, 0x00), TAG, "fifo reset release");
    uint8_t status;
    return read_regs(B0_INT_STATUS_2, &status, 1);  // clear any stale overflow flag
}

esp_err_t accel_fifo_read(accel_sample_t *out, size_t max, size_t *got)
{
    static uint8_t raw[FIFO_READ_MAX * FIFO_FRAME_BYTES];
    *got = 0;

    uint8_t status;
    ESP_RETURN_ON_ERROR(read_regs(B0_INT_STATUS_2, &status, 1), TAG, "int status 2");
    if (status & 0x1F) {
        return ESP_ERR_INVALID_STATE;
    }

    // One 2-byte read so high and low bytes describe the same moment.
    uint8_t c[2];
    ESP_RETURN_ON_ERROR(read_regs(B0_FIFO_COUNTH, c, sizeof(c)), TAG, "fifo count");
    size_t frames = (((c[0] & 0x1F) << 8) | c[1]) / FIFO_FRAME_BYTES;  // whole frames only
    if (frames > max) frames = max;
    if (frames > FIFO_READ_MAX) frames = FIFO_READ_MAX;  // rest stays queued for the next call
    if (frames == 0) {
        return ESP_OK;
    }

    ESP_RETURN_ON_ERROR(read_regs(B0_FIFO_R_W, raw, frames * FIFO_FRAME_BYTES), TAG, "fifo data");
    for (size_t i = 0; i < frames; i++) {
        decode(&raw[i * FIFO_FRAME_BYTES], &out[i]);
    }
    *got = frames;
    return ESP_OK;
}
