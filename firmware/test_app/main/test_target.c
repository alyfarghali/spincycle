#include <math.h>
#include <string.h>
#include "unity.h"
#include "backoff.h"
#include "rms.h"
#include "telemetry.h"
#include "suites.h"

static void test_telemetry_matches_contract_on_target(void)
{
    char buf[256];
    struct timeval tv = {1792504991, 250000};
    int n = telemetry_build(buf, sizeof buf, "c6-washer-03", "0.1.0", 12, 1842, &tv, 0.0423f);
    TEST_ASSERT_EQUAL_STRING("{\"v\":1,\"device_id\":\"c6-washer-03\",\"fw\":\"0.1.0\",\"boot\":12,\"seq\":1842,"
                             "\"ts\":\"2026-10-20T14:03:11.250Z\",\"metrics\":{\"vibration_rms\":0.0423}}", buf);
    TEST_ASSERT_EQUAL(strlen(buf), n);

    struct timeval late = {1835481599, 999999};
    TEST_ASSERT_GREATER_THAN(0, telemetry_build(buf, sizeof buf, "a", "0", 1, 9007199254740992ULL, &late, 0.0001234f));
    TEST_ASSERT_NOT_NULL(strstr(buf, "\"ts\":\"2028-02-29T23:59:59.999Z\""));
    TEST_ASSERT_NOT_NULL(strstr(buf, "\"seq\":9007199254740992"));
    TEST_ASSERT_NOT_NULL(strstr(buf, "\"vibration_rms\":0.0001234}"));

    TEST_ASSERT_EQUAL(-1, telemetry_build(buf, sizeof buf, "a", "0", 1, 1, NULL, NAN));
    TEST_ASSERT_EQUAL(-1, telemetry_build(buf, 20, "a", "0", 1, 1, NULL, 0.01f));
}

static void test_rms_matches_contract_on_target(void)
{
    static float x[500], y[500], z[500];
    for (int i = 0; i < 500; i++) {
        x[i] = 0;
        y[i] = 0;
        z[i] = 1 + 0.05f * sinf(2.0f * (float)M_PI * 10.0f * i / 100.0f);
    }
    TEST_ASSERT_FLOAT_WITHIN(1e-4f, 0.05f / sqrtf(2), rms_window(x, y, z, 500));
}

static void test_backoff_matches_contract_on_target(void)
{
    const uint32_t base[] = {1000, 2000, 4000, 8000, 16000, 32000, 60000, 60000};
    for (uint32_t a = 0; a < 8; a++) {
        TEST_ASSERT_EQUAL_UINT32(base[a], backoff_ms(a, 0.5f));
        TEST_ASSERT_EQUAL_UINT32(base[a] * 8 / 10, backoff_ms(a, 0.0f));
    }
    TEST_ASSERT_EQUAL_UINT32(60000, backoff_ms(UINT32_MAX, 0.5f));
}

void run_target_tests(void)
{
    RUN_TEST(test_telemetry_matches_contract_on_target);
    RUN_TEST(test_rms_matches_contract_on_target);
    RUN_TEST(test_backoff_matches_contract_on_target);
}
