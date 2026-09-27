package ru.viktortown.yarus;

/** Pure validation and stability rules shared by the native camera flow. */
public final class BarcodeScanPolicy {
    public static final int REQUIRED_MATCHES = 3;
    private static final long SERIES_TIMEOUT_MS = 2500L;

    public static final class Result {
        public final String value;
        public final String format;

        Result(String value, String format) {
            this.value = value;
            this.format = format;
        }
    }

    private String candidate = "";
    private String candidateFormat = "";
    private int matches = 0;
    private long lastSeenAt = 0L;

    public Result observe(String rawValue, String rawFormat, long now) {
        String value = clean(rawValue);
        String format = normalizeFormat(rawFormat);
        if (value == null || format == null || !validForFormat(value, format)) {
            return null;
        }
        if (value.equals(candidate) && format.equals(candidateFormat)
                && now - lastSeenAt <= SERIES_TIMEOUT_MS) {
            matches++;
        } else {
            candidate = value;
            candidateFormat = format;
            matches = 1;
        }
        lastSeenAt = now;
        return matches >= REQUIRED_MATCHES ? new Result(value, format) : null;
    }

    public void reset() {
        candidate = "";
        candidateFormat = "";
        matches = 0;
        lastSeenAt = 0L;
    }

    static String clean(String value) {
        if (value == null) return null;
        String text = value.trim();
        if (text.isEmpty() || text.length() > 512) return null;
        for (int index = 0; index < text.length(); index++) {
            char character = text.charAt(index);
            if (character < 0x20 || character == 0x7f) return null;
        }
        return text;
    }

    static String normalizeFormat(String value) {
        if (value == null) return null;
        switch (value.trim().toUpperCase()) {
            case "QR":
            case "QR_CODE": return "QR";
            case "EAN13":
            case "EAN_13":
            case "EAN-13": return "EAN-13";
            case "EAN8":
            case "EAN_8":
            case "EAN-8": return "EAN-8";
            case "CODE128":
            case "CODE_128":
            case "CODE 128": return "Code 128";
            default: return null;
        }
    }

    static boolean validForFormat(String value, String format) {
        if ("EAN-13".equals(format)) return value.matches("\\d{13}") && validEan(value);
        if ("EAN-8".equals(format)) return value.matches("\\d{8}") && validEan(value);
        return "QR".equals(format) || "Code 128".equals(format);
    }

    static boolean validEan(String value) {
        if (value == null || !(value.matches("\\d{8}") || value.matches("\\d{13}"))) return false;
        int sum = 0;
        for (int index = value.length() - 2, position = 0; index >= 0; index--, position++) {
            sum += (value.charAt(index) - '0') * (position % 2 == 0 ? 3 : 1);
        }
        return (10 - sum % 10) % 10 == value.charAt(value.length() - 1) - '0';
    }
}
