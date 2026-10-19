# sensing component: accelerometer driver, RMS math, sensor task.
idf_component_register(SRCS "accel.c" "rms.c" "sensor_task.c"
                       INCLUDE_DIRS "include"
                       REQUIRES esp_driver_i2c esp_driver_gpio)
