package ru.viktortown.yarus;
import android.webkit.JavascriptInterface;
public class Bridge {
    public MainActivity activity;
    public Bridge(MainActivity owner) { activity = owner; }
    @JavascriptInterface public void saveText(String name, String text, String mime) {
        activity.runOnUiThread(new ExportTask(activity, name, text, mime));
    }
    @JavascriptInterface public void closeApp() {
        activity.runOnUiThread(new ExportTask(activity, null, null, null));
    }
}
