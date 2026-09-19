package ru.viktortown.yarus;

import android.app.Activity;
import android.os.Bundle;
import android.os.Build;
import android.content.Intent;
import android.webkit.WebView;
import android.webkit.WebSettings;
import android.webkit.WebChromeClient;
import android.webkit.ValueCallback;
import android.net.Uri;
import android.widget.Toast;
import android.view.WindowInsets;
import android.window.OnBackInvokedDispatcher;
import java.util.Scanner;
import java.io.OutputStream;

/** Local assets, persistent WebView storage, and explicit user-selected file IO.
 * No remote website is loaded into the bridge-enabled WebView.
 */
public class MainActivity extends Activity {
    public WebView web;
    public android.webkit.PermissionRequest cameraRequest;
    public String pendingText;
    public ValueCallback<Uri[]> fileCallback;

    @Override protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        try {
            requestWindowFeature(1);
            getWindow().setStatusBarColor(0xff153f3b);
            getWindow().setNavigationBarColor(0xff153f3b);
            if (Build.VERSION.SDK_INT >= 35) {
                getWindow().setNavigationBarContrastEnforced(false);
            }
            web = new WebView(this);
            WebSettings settings = web.getSettings();
            settings.setJavaScriptEnabled(true);
            settings.setDomStorageEnabled(true);
            settings.setMediaPlaybackRequiresUserGesture(false);
            settings.setAllowFileAccess(false);
            settings.setAllowFileAccessFromFileURLs(false);
            settings.setAllowUniversalAccessFromFileURLs(false);
            if (Build.VERSION.SDK_INT >= 26) settings.setSafeBrowsingEnabled(true);
            // Access only content URIs explicitly selected through the system file picker.
            settings.setAllowContentAccess(true);
            // The app validates plain HTTP endpoints as private literal IPs only.
            settings.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);
            web.setWebViewClient(new GuardClient());
            web.setWebChromeClient(new ChromeClient(this));
            web.addJavascriptInterface(new Bridge(this), "AndroidFiles");
            setContentView(web);
            applySystemInsets();
            if (Build.VERSION.SDK_INT >= 33) {
                getOnBackInvokedDispatcher().registerOnBackInvokedCallback(
                        OnBackInvokedDispatcher.PRIORITY_DEFAULT, this::handleBack);
            }
            Scanner scanner = new Scanner(getAssets().open("index.html"), "UTF-8");
            scanner.useDelimiter("\\A");
            String html = scanner.next();
            scanner.close();
            web.loadDataWithBaseURL("https://appassets.androidplatform.net/", html,
                    "text/html", "UTF-8", null);
        } catch (Exception error) {
            Toast.makeText(this, "Не удалось открыть ЯРУС. Обновите Android System WebView.", Toast.LENGTH_LONG).show();
            finish();
        }
    }

    private void applySystemInsets() {
        web.setOnApplyWindowInsetsListener((view, insets) -> {
            if (Build.VERSION.SDK_INT >= 30) {
                android.graphics.Insets bars = insets.getInsets(
                        WindowInsets.Type.statusBars() | WindowInsets.Type.navigationBars());
                view.setPadding(bars.left, bars.top, bars.right, bars.bottom);
            } else {
                view.setPadding(insets.getSystemWindowInsetLeft(), insets.getSystemWindowInsetTop(),
                        insets.getSystemWindowInsetRight(), insets.getSystemWindowInsetBottom());
            }
            return insets;
        });
    }

    private void handleBack() {
        if (web == null) { finish(); return; }
        web.evaluateJavascript("if(typeof yarusScanSession!=='undefined'&&yarusScanSession){closeScanner();}else if(document.querySelector('#modal-root .modal')){closeModal();}else if(typeof page==='string'&&page!=='home'&&state){page='home';render();}else{AndroidFiles.closeApp();}", null);
    }

    @SuppressWarnings("deprecation")
    @Override public void onBackPressed() {
        if (Build.VERSION.SDK_INT < 33) handleBack();
        else super.onBackPressed();
    }

    /** Called on the UI thread by CameraPermissionTask. Only our local origin and video. */
    public void handleCameraRequest(android.webkit.PermissionRequest request) {
        Uri origin = request.getOrigin();
        String[] resources = request.getResources();
        if (!"https".equals(origin.getScheme()) || !"appassets.androidplatform.net".equals(origin.getHost())
                || (origin.getPort() != -1 && origin.getPort() != 443) || resources.length != 1
                || !android.webkit.PermissionRequest.RESOURCE_VIDEO_CAPTURE.equals(resources[0])) {
            request.deny(); return;
        }
        if (cameraRequest != null) cameraRequest.deny();
        cameraRequest = request;
        if (checkSelfPermission("android.permission.CAMERA") == 0) {
            request.grant(new String[]{android.webkit.PermissionRequest.RESOURCE_VIDEO_CAPTURE});
            cameraRequest = null;
        } else requestPermissions(new String[]{"android.permission.CAMERA"}, 1003);
    }
    @Override public void onRequestPermissionsResult(int code, String[] permissions, int[] results) {
        super.onRequestPermissionsResult(code, permissions, results);
        if (code == 1003 && cameraRequest != null) {
            if (checkSelfPermission("android.permission.CAMERA") == 0)
                cameraRequest.grant(new String[]{android.webkit.PermissionRequest.RESOURCE_VIDEO_CAPTURE});
            else cameraRequest.deny();
            cameraRequest = null;
        }
    }
    @Override protected void onPause() {
        if (web != null) web.evaluateJavascript("if(typeof pauseScanner==='function')pauseScanner();", null);
        super.onPause();
    }
    @Override protected void onDestroy() {
        if (cameraRequest != null) { cameraRequest.deny(); cameraRequest = null; }

        if (web != null) web.destroy();
        super.onDestroy();
    }

    @Override protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        super.onActivityResult(requestCode, resultCode, data);
        if (requestCode == 1002) {
            if (fileCallback != null) {
                fileCallback.onReceiveValue(WebChromeClient.FileChooserParams.parseResult(resultCode, data));
                fileCallback = null;
            }
            return;
        }
        if (requestCode != 1001) return;
        String message = "Сохранение отменено.";
        if (resultCode == RESULT_OK && data != null && pendingText != null) {
            try {
                Uri uri = data.getData();
                if (uri != null) {
                    OutputStream stream = getContentResolver().openOutputStream(uri, "wt");
                    stream.write(pendingText.getBytes("UTF-8"));
                    stream.close();
                    message = "Файл сохранён.";
                }
            } catch (Exception error) {
                message = "Ошибка сохранения. Повторите экспорт в другой файл.";
            }
        }
        pendingText = null;
        Toast.makeText(this, message, Toast.LENGTH_LONG).show();
    }
}
