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
    @JavascriptInterface public void updateWidget(int items, int low, String value, String workspace) {
        activity.runOnUiThread(() -> YarusWidgetProvider.updateAll(activity, items, low, value, workspace));
    }
    @JavascriptInterface public String loadHostState() { return activity.loadHostState(); }
    @JavascriptInterface public boolean saveHostState(String json) { return activity.saveHostState(json); }
    @JavascriptInterface public long hostStateSize() { return activity.hostStateSize(); }
    @JavascriptInterface public long hostBackupSize() { return activity.hostBackupSize(); }
    @JavascriptInterface public int hostBackupCount() { return activity.hostBackupCount(); }
    @JavascriptInterface public String startHostServer() { return activity.startHostServer(); }
    @JavascriptInterface public void stopHostServer() { activity.stopHostServer(); }
    @JavascriptInterface public void hostRespond(String id, int status, String body) {
        activity.completeHostResponse(id, status, body);
    }
    @JavascriptInterface public void requestReview() { activity.runOnUiThread(activity::requestStoreReview); }
    @JavascriptInterface public boolean openExternal(String value) { return activity.openExternal(value); }
    @JavascriptInterface public void createPortable(String name, String text) {
        activity.runOnUiThread(() -> activity.beginPortableCreate(name, text));
    }
    @JavascriptInterface public void openPortable() {
        activity.runOnUiThread(activity::beginPortableOpen);
    }
    @JavascriptInterface public String portableInfo() { return activity.portableFile.info(); }
    @JavascriptInterface public String writePortable(String text) { return activity.portableFile.writeAttached(text); }
    @JavascriptInterface public String confirmPortable() {
        try { return activity.portableFile.confirmPending(); }
        catch (Exception error) { return "!ERROR:" + error.getMessage(); }
    }
    @JavascriptInterface public void cancelPortable() { activity.portableFile.cancelPending(); }
    @JavascriptInterface public void detachPortable() { activity.portableFile.detach(); }
}
