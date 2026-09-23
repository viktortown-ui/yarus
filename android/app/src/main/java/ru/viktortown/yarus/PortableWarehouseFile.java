package ru.viktortown.yarus;

import android.content.ContentResolver;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.database.Cursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;

import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;

/**
 * A user-owned portable warehouse file selected through Android's Storage Access Framework.
 *
 * The URI permission and display name are app preferences, while the JSON document itself is
 * outside the app sandbox and therefore survives uninstall. The internal IndexedDB remains the
 * primary crash-safe working copy; this document is an automatically refreshed recovery copy.
 */
public final class PortableWarehouseFile {
    public static final int REQUEST_CREATE = 1004;
    public static final int REQUEST_OPEN = 1005;
    private static final long MAX_BYTES = 256L << 20;
    private static final String PREFS = "yarus_portable_file";
    private static final String PREF_URI = "uri";
    private static final String PREF_NAME = "name";

    private final Context context;
    private Uri pendingUri;
    private int pendingFlags;
    private String pendingName = "";

    public PortableWarehouseFile(Context value) {
        context = value.getApplicationContext();
    }

    public synchronized void stage(Uri uri, int flags) throws Exception {
        if (uri == null || !"content".equalsIgnoreCase(uri.getScheme())) {
            throw new IllegalArgumentException("Выбранный файл недоступен через безопасное хранилище Android.");
        }
        int allowed = flags & (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
        if ((allowed & Intent.FLAG_GRANT_READ_URI_PERMISSION) == 0) {
            throw new IllegalStateException("Хранилище не дало доступ к выбранному файлу.");
        }
        context.getContentResolver().takePersistableUriPermission(uri, allowed);
        pendingUri = uri;
        pendingFlags = allowed;
        pendingName = displayName(uri);
    }

    public synchronized String confirmPending() throws Exception {
        if (pendingUri == null) throw new IllegalStateException("Сначала выберите файл склада.");
        if ((pendingFlags & Intent.FLAG_GRANT_WRITE_URI_PERMISSION) == 0) {
            throw new IllegalStateException("Файл открыт только для чтения. Выберите папку, в которой разрешено сохранение.");
        }
        preferences().edit().putString(PREF_URI, pendingUri.toString())
                .putString(PREF_NAME, pendingName).commit();
        clearPending(false);
        return info();
    }

    public synchronized void cancelPending() {
        clearPending(true);
    }

    public synchronized String attachCreated(Uri uri, int flags, String json) throws Exception {
        stage(uri, flags);
        if ((pendingFlags & Intent.FLAG_GRANT_WRITE_URI_PERMISSION) == 0) {
            cancelPending();
            throw new IllegalStateException("Хранилище не разрешило записать файл склада.");
        }
        write(uri, json);
        return confirmPending();
    }

    public synchronized String readPending() throws Exception {
        if (pendingUri == null) throw new IllegalStateException("Файл склада не выбран.");
        return read(pendingUri);
    }

    public synchronized String pendingName() { return pendingName; }

    /** Returns an empty string on success and a human-readable error otherwise. */
    public synchronized String writeAttached(String json) {
        String value = preferences().getString(PREF_URI, "");
        if (value.isEmpty()) return "Файл склада не подключён.";
        try {
            write(Uri.parse(value), json);
            return "";
        } catch (Exception error) {
            return "Внутренняя база сохранена, но внешний файл не обновлён: " + safeMessage(error);
        }
    }

    public synchronized String info() {
        SharedPreferences preferences = preferences();
        String value = preferences.getString(PREF_URI, "");
        String name = preferences.getString(PREF_NAME, "");
        JSONObject result = new JSONObject();
        try {
            result.put("attached", !value.isEmpty());
            result.put("name", name);
            result.put("uri", value);
            if (!value.isEmpty()) {
                Uri uri = Uri.parse(value);
                result.put("name", displayName(uri));
                result.put("bytes", size(uri));
            }
        } catch (Exception error) {
            try { result.put("error", safeMessage(error)); } catch (Exception ignored) {}
        }
        return result.toString();
    }

    public synchronized void detach() {
        String value = preferences().getString(PREF_URI, "");
        if (!value.isEmpty()) {
            try {
                context.getContentResolver().releasePersistableUriPermission(Uri.parse(value),
                        Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            } catch (Exception ignored) {}
        }
        preferences().edit().clear().commit();
    }

    private void write(Uri uri, String json) throws Exception {
        if (json == null || json.isEmpty()) throw new IllegalArgumentException("Нет данных склада для сохранения.");
        byte[] bytes = json.getBytes(StandardCharsets.UTF_8);
        if (bytes.length > MAX_BYTES) throw new IllegalArgumentException("Файл склада больше 256 МБ.");
        ContentResolver resolver = context.getContentResolver();
        try (ParcelFileDescriptor descriptor = resolver.openFileDescriptor(uri, "rwt")) {
            if (descriptor == null) throw new IllegalStateException("Android не открыл файл для записи.");
            try (FileOutputStream output = new FileOutputStream(descriptor.getFileDescriptor())) {
                output.write(bytes);
                output.flush();
                output.getFD().sync();
            }
        }
    }

    private String read(Uri uri) throws Exception {
        try (InputStream input = context.getContentResolver().openInputStream(uri);
             ByteArrayOutputStream output = new ByteArrayOutputStream()) {
            if (input == null) throw new IllegalStateException("Android не открыл выбранный файл.");
            byte[] buffer = new byte[32 * 1024];
            int count;
            while ((count = input.read(buffer)) != -1) {
                if (count == 0) continue;
                output.write(buffer, 0, count);
                if (output.size() > MAX_BYTES) throw new IllegalStateException("Файл склада больше 256 МБ.");
            }
            return output.toString(StandardCharsets.UTF_8.name());
        }
    }

    private long size(Uri uri) {
        try (Cursor cursor = context.getContentResolver().query(uri,
                new String[]{OpenableColumns.SIZE}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst() && !cursor.isNull(0)) return cursor.getLong(0);
        } catch (Exception ignored) {}
        return -1;
    }

    private String displayName(Uri uri) {
        try (Cursor cursor = context.getContentResolver().query(uri,
                new String[]{OpenableColumns.DISPLAY_NAME}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst()) {
                String value = cursor.getString(0);
                if (value != null && !value.trim().isEmpty()) return value.trim();
            }
        } catch (Exception ignored) {}
        String tail = uri.getLastPathSegment();
        return tail == null || tail.isEmpty() ? "Файл склада ЯРУС" : tail;
    }

    private void clearPending(boolean release) {
        if (release && pendingUri != null) {
            try { context.getContentResolver().releasePersistableUriPermission(pendingUri, pendingFlags); }
            catch (Exception ignored) {}
        }
        pendingUri = null;
        pendingFlags = 0;
        pendingName = "";
    }

    private SharedPreferences preferences() {
        return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    private static String safeMessage(Exception error) {
        String value = error.getMessage();
        return value == null || value.trim().isEmpty() ? "файл недоступен" : value.trim();
    }
}
