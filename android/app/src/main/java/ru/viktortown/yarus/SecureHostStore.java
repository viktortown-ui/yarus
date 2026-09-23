package ru.viktortown.yarus;

import android.content.Context;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.system.Os;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.text.SimpleDateFormat;
import java.util.Arrays;
import java.util.Date;
import java.util.Locale;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** Encrypted, atomic storage for an Android-hosted warehouse.
 * The AES key is non-exportable and lives in Android Keystore.
 */
public final class SecureHostStore {
    private static final String ALIAS = "yarus_host_state_v1";
    private static final byte[] MAGIC = new byte[]{'Y', 'H', 'S', 'T', 1};
    private static final long MAX_BYTES = 256L << 20;
    private static final long MAX_BACKUP_BYTES = 512L << 20;
    private static final int MAX_BACKUP_FILES = 14;
    private static final int MIN_BACKUP_FILES = 2;
    private final Context context;
    private final File stateFile;
    private final File backupDir;

    public SecureHostStore(Context context) {
        this.context = context.getApplicationContext();
        this.stateFile = new File(context.getFilesDir(), "host-state.yarus.enc");
        this.backupDir = new File(context.getFilesDir(), "host-backups");
    }

    public synchronized boolean exists() { return stateFile.isFile() && stateFile.length() > MAGIC.length + 12; }
    public synchronized long size() { return exists() ? stateFile.length() : 0L; }
    public synchronized int backupCount() { return backupFiles().length; }
    public synchronized long backupSize() {
        long total = 0L;
        for (File file : backupFiles()) total += Math.max(0L, file.length());
        return total;
    }

    public synchronized String load() throws Exception {
        if (!exists()) return "";
        byte[] raw = readAll(stateFile, MAX_BYTES + MAGIC.length + 28);
        if (raw.length < MAGIC.length + 12 + 16 || !Arrays.equals(MAGIC, Arrays.copyOf(raw, MAGIC.length))) {
            throw new IllegalStateException("Файл главного склада повреждён. Не удаляйте данные приложения.");
        }
        byte[] iv = Arrays.copyOfRange(raw, MAGIC.length, MAGIC.length + 12);
        byte[] encrypted = Arrays.copyOfRange(raw, MAGIC.length + 12, raw.length);
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, iv));
        cipher.updateAAD(MAGIC);
        return new String(cipher.doFinal(encrypted), StandardCharsets.UTF_8);
    }

    public synchronized void save(String json) throws Exception {
        byte[] plain = json == null ? new byte[0] : json.getBytes(StandardCharsets.UTF_8);
        if (plain.length == 0 || plain.length > MAX_BYTES) {
            throw new IllegalArgumentException("Недопустимый размер базы главного устройства.");
        }
        maybeDailyBackup();
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.ENCRYPT_MODE, key());
        cipher.updateAAD(MAGIC);
        byte[] iv = cipher.getIV();
        byte[] encrypted = cipher.doFinal(plain);
        File temp = new File(stateFile.getParentFile(), ".host-state-" + System.nanoTime() + ".tmp");
        boolean done = false;
        try (FileOutputStream output = new FileOutputStream(temp)) {
            output.write(MAGIC);
            output.write(iv);
            output.write(encrypted);
            output.getFD().sync();
            done = true;
        } finally {
            if (!done) temp.delete();
        }
        try {
            // POSIX rename replaces the destination atomically: after a crash there is
            // either the complete old database or the complete new database.
            Os.rename(temp.getAbsolutePath(), stateFile.getAbsolutePath());
        } catch (Exception error) {
            temp.delete();
            throw new IllegalStateException("Не удалось завершить сохранение базы.", error);
        }
    }

    private SecretKey key() throws Exception {
        KeyStore store = KeyStore.getInstance("AndroidKeyStore");
        store.load(null);
        java.security.Key current = store.getKey(ALIAS, null);
        if (current instanceof SecretKey) return (SecretKey) current;
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
        generator.init(new KeyGenParameterSpec.Builder(ALIAS,
                KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build());
        return generator.generateKey();
    }

    private void maybeDailyBackup() throws Exception {
        if (!exists()) return;
        String day = new SimpleDateFormat("yyyy-MM-dd", Locale.ROOT).format(new Date());
        SharedPreferences preferences = context.getSharedPreferences("yarus_secure_store", Context.MODE_PRIVATE);
        if (day.equals(preferences.getString("backup_day", ""))) {
            pruneBackups();
            return;
        }
        if (!backupDir.exists() && !backupDir.mkdirs()) throw new IllegalStateException("Не удалось создать папку внутренних копий.");
        makeRoomForBackup(stateFile.length());
        File destination = new File(backupDir, "host-" + day + ".yarus.enc");
        copyFile(stateFile, destination);
        preferences.edit().putString("backup_day", day).apply();
        pruneBackups();
    }

    private File[] backupFiles() {
        File[] files = backupDir.listFiles((dir, name) -> name.startsWith("host-") && name.endsWith(".yarus.enc"));
        return files == null ? new File[0] : files;
    }

    private void pruneBackups() {
        File[] files = backupFiles();
        Arrays.sort(files, (a, b) -> a.getName().compareTo(b.getName()));
        long total = 0L;
        for (File file : files) total += Math.max(0L, file.length());
        int remaining = files.length;
        for (File file : files) {
            if (remaining <= MIN_BACKUP_FILES) break;
            if (remaining <= MAX_BACKUP_FILES && total <= MAX_BACKUP_BYTES) break;
            long length = Math.max(0L, file.length());
            if (file.delete()) {
                total -= length;
                remaining--;
            }
        }
    }

    private void makeRoomForBackup(long incomingBytes) {
        File[] files = backupFiles();
        Arrays.sort(files, (a, b) -> a.getName().compareTo(b.getName()));
        long total = 0L;
        for (File file : files) total += Math.max(0L, file.length());
        int remaining = files.length;
        for (File file : files) {
            // Keep one older recovery point while the new atomic copy is made.
            // After a successful copy there will again be at least two.
            if (remaining <= 1) break;
            if (remaining < MAX_BACKUP_FILES && total + incomingBytes <= MAX_BACKUP_BYTES) break;
            long length = Math.max(0L, file.length());
            if (file.delete()) {
                total -= length;
                remaining--;
            }
        }
    }

    private static void copyFile(File source, File destination) throws Exception {
        File temp = new File(destination.getParentFile(), ".backup-" + System.nanoTime() + ".tmp");
        try (FileInputStream input = new FileInputStream(source); FileOutputStream output = new FileOutputStream(temp)) {
            byte[] buffer = new byte[32 * 1024];
            int count;
            while ((count = input.read(buffer)) >= 0) output.write(buffer, 0, count);
            output.getFD().sync();
        }
        try {
            Os.rename(temp.getAbsolutePath(), destination.getAbsolutePath());
        } catch (Exception error) {
            temp.delete();
            throw new IllegalStateException("Не удалось завершить внутреннюю копию.", error);
        }
    }

    private static byte[] readAll(File file, long limit) throws Exception {
        if (file.length() > limit) throw new IllegalStateException("Файл главного склада слишком большой.");
        try (FileInputStream input = new FileInputStream(file); ByteArrayOutputStream output = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[32 * 1024];
            int count;
            while ((count = input.read(buffer)) >= 0) {
                output.write(buffer, 0, count);
                if (output.size() > limit) throw new IllegalStateException("Файл главного склада слишком большой.");
            }
            return output.toByteArray();
        }
    }
}
