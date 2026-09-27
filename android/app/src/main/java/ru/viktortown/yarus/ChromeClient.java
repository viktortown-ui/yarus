package ru.viktortown.yarus;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.Uri;
import android.webkit.JsResult;
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
    @Override public boolean onJsConfirm(WebView view, String url, String message, JsResult result) {
        activity.runOnUiThread(() -> new AlertDialog.Builder(activity)
                .setTitle("ЯРУС")
                .setMessage(message)
                .setPositiveButton("Да", (dialog, which) -> result.confirm())
                .setNegativeButton("Нет", (dialog, which) -> result.cancel())
                .setOnCancelListener(dialog -> result.cancel())
                .show());
        return true;
    }
    @Override public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback, FileChooserParams params) {
        if (activity.fileCallback != null) activity.fileCallback.onReceiveValue(null);
        activity.fileCallback = callback;
        Intent intent = new Intent(Intent.ACTION_GET_CONTENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        String[] accepted = params == null ? null : params.getAcceptTypes();
        intent.setType(accepted != null && accepted.length == 1 && accepted[0] != null
                && !accepted[0].isEmpty() ? accepted[0] : "*/*");
        activity.startActivityForResult(intent, 1002);
        return true;
    }
}
