package ru.viktortown.yarus;
import android.content.Intent;
import android.widget.Toast;
public class ExportTask implements Runnable {
    public MainActivity activity;
    public String name, text, mime;
    public ExportTask(MainActivity owner, String fileName, String content, String contentType) {
        activity = owner; name = fileName; text = content; mime = contentType;
    }
    @Override public void run() {
        if (name == null) { activity.finish(); return; }
        try {
            activity.pendingText = text;
            Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
            intent.addCategory(Intent.CATEGORY_OPENABLE);
            intent.setType(mime);
            intent.putExtra(Intent.EXTRA_TITLE, name);
            activity.startActivityForResult(intent, 1001);
        } catch (Exception error) {
            activity.pendingText = null;
            Toast.makeText(activity, "Не удалось открыть окно сохранения.", Toast.LENGTH_LONG).show();
        }
    }
}
