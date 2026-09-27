package ru.viktortown.yarus;

/** Runs without Android or JUnit, so it also works when the project path contains Cyrillic. */
public final class BarcodeScanPolicySelfTest {
    private static void check(boolean value, String message) {
        if (!value) throw new AssertionError(message);
    }

    public static void main(String[] args) {
        BarcodeScanPolicy policy = new BarcodeScanPolicy();
        check(policy.observe("4601234567893", "EAN_13", 1000) == null, "first frame must wait");
        check(policy.observe("4601234567893", "EAN_13", 1200) == null, "second frame must wait");
        BarcodeScanPolicy.Result stable = policy.observe("4601234567893", "EAN_13", 1400);
        check(stable != null, "third matching frame must be accepted");
        check("4601234567893".equals(stable.value), "EAN value changed");
        check("EAN-13".equals(stable.format), "EAN format was not normalized");

        policy.reset();
        check(policy.observe("4601234567893", "EAN_13", 1000) == null, "series start failed");
        check(policy.observe("12345670", "EAN_8", 1200) == null, "conflicting frame must restart");
        check(policy.observe("4601234567893", "EAN_13", 1400) == null, "new series first frame accepted");
        check(policy.observe("4601234567893", "EAN_13", 1600) == null, "new series second frame accepted");
        check(policy.observe("4601234567893", "EAN_13", 1800) != null, "new series third frame rejected");

        policy.reset();
        check(policy.observe("4601234567893", "EAN_13", 1000) == null, "timeout series start failed");
        check(policy.observe("4601234567893", "EAN_13", 4000) == null, "expired series must restart");
        check(policy.observe("4601234567894", "EAN_13", 4200) == null, "bad EAN checksum accepted");
        check(policy.observe("bad\nvalue", "CODE_128", 4400) == null, "control character accepted");
        check(BarcodeScanPolicy.validEan("12345670"), "valid EAN-8 rejected");
        check(!BarcodeScanPolicy.validEan("12345671"), "invalid EAN-8 accepted");
        System.out.println("PASS: native scanner validation, checksum and three-frame stability");
    }
}
