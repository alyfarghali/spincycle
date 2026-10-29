# sensing component: accelerometer driver, RMS math, sensor task.
# CONFIG_SPINCYCLE_FAKE_ACCEL swaps the real driver for a synthetic one (bench work without the sensor).
if(CONFIG_SPINCYCLE_FAKE_ACCEL)
    set(accel_src "accel_fake.c")
else()
    set(accel_src "accel.c")
endif()

idf_component_register(SRCS "${accel_src}" "rms.c" "sensor_task.c"
                       INCLUDE_DIRS "include"
                       REQUIRES esp_driver_i2c esp_driver_gpio esp_timer)
