package ru.viktortown.yarus;
import android.content.Intent;
import android.net.Uri;
import android.webkit.WebChromeClient;
import android.webkit.WebView;
import android.webkit.ValueCallback;
public class ChromeClient extends WebChromeClient {
    public MainActivity activity;
    public ChromeClient(MainActivity owner) { activity = owner; }
    @Override public void onPermissionRequest(android.webkit.PermissionRequest request) {
        activity.runOnUiThread(new CameraPermissionTask(activity, request));
    }
    @Override public void onPermissionRequestCanceled(android.webkit.PermissionRequest request) {
        if (activity.cameraRequest == request) activity.cameraRequest = null;
    }
    @Override public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback, FileChooserParams params) {
        if (activity.fileCallback != null) activity.fileCallback.onReceiveValue(null);
        activity.fileCallback = callback;
        Intent intent = new Intent(Intent.ACTION_GET_CONTENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("*/*");
        activity.startActivityForResult(intent, 1002);
        return true;
    }
}
