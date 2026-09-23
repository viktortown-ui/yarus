package ru.viktortown.yarus;

import android.app.Activity;
import android.annotation.SuppressLint;
import android.content.pm.PackageManager;
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
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import org.json.JSONObject;

/** Local assets, persistent WebView storage, and explicit user-selected file IO.
 * No remote website is loaded into the bridge-enabled WebView.
 */
public class MainActivity extends Activity {
    public WebView web;
    public android.webkit.PermissionRequest cameraRequest;
    public String pendingText;
    public ValueCallback<Uri[]> fileCallback;
    public SecureHostStore hostStore;
    public MobileHostServer hostServer;
    public PortableWarehouseFile portableFile;
    public String pendingPortableText;
    private final ConcurrentHashMap<String, CompletableFuture<HostResponse>> hostResponses = new ConcurrentHashMap<>();
    private final AtomicLong hostRequestCounter = new AtomicLong();

    public static final class HostResponse {
        public final int status;
        public final String body;
        HostResponse(int status, String body) { this.status = status; this.body = body; }
    }

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
            hostStore = new SecureHostStore(this);
            hostServer = new MobileHostServer(this);
            portableFile = new PortableWarehouseFile(this);
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

    @SuppressLint("GestureBackNavigation") // API 33+ is handled by OnBackInvokedDispatcher above.
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
        if (checkSelfPermission("android.permission.CAMERA") == PackageManager.PERMISSION_GRANTED) {
            request.grant(new String[]{android.webkit.PermissionRequest.RESOURCE_VIDEO_CAPTURE});
            cameraRequest = null;
        } else requestPermissions(new String[]{"android.permission.CAMERA"}, 1003);
    }
    @Override public void onRequestPermissionsResult(int code, String[] permissions, int[] results) {
        super.onRequestPermissionsResult(code, permissions, results);
        if (code == 1003 && cameraRequest != null) {
            if (checkSelfPermission("android.permission.CAMERA") == PackageManager.PERMISSION_GRANTED)
                cameraRequest.grant(new String[]{android.webkit.PermissionRequest.RESOURCE_VIDEO_CAPTURE});
            else cameraRequest.deny();
            cameraRequest = null;
        }
    }
    @Override protected void onPause() {
        if (web != null) web.evaluateJavascript("if(typeof pauseScanner==='function')pauseScanner();", null);
        super.onPause();
    }
    @Override protected void onStart() {
        super.onStart();
        if (web != null) web.evaluateJavascript("if(window.YarusAndroidHost)YarusAndroidHost.resumeNative();", null);
    }
    @Override protected void onStop() {
        if (hostServer != null) hostServer.stop();
        if (web != null) web.evaluateJavascript("if(window.YarusAndroidHost)YarusAndroidHost.nativeStopped();", null);
        super.onStop();
    }
    @Override protected void onDestroy() {
        if (cameraRequest != null) { cameraRequest.deny(); cameraRequest = null; }
        if (hostServer != null) hostServer.destroy();
        for (CompletableFuture<HostResponse> future : hostResponses.values())
            future.complete(new HostResponse(503, "{\"error\":\"Главное приложение закрывается.\"}"));
        hostResponses.clear();
        if (web != null) web.destroy();
        super.onDestroy();
    }

    public String loadHostState() {
        try { return hostStore == null ? "" : hostStore.load(); }
        catch (Exception error) { return "!ERROR:" + error.getMessage(); }
    }

    public boolean saveHostState(String json) {
        try { hostStore.save(json); return true; }
        catch (Exception error) { reportHostError("Не удалось сохранить главный склад: " + error.getMessage()); return false; }
    }

    public long hostStateSize() { return hostStore == null ? 0L : hostStore.size(); }
    public long hostBackupSize() { return hostStore == null ? 0L : hostStore.backupSize(); }
    public int hostBackupCount() { return hostStore == null ? 0 : hostStore.backupCount(); }

    public String startHostServer() {
        try { return hostServer.start(); }
        catch (Exception error) { reportHostError("Не удалось запустить общий склад: " + error.getMessage()); return "[]"; }
    }

    public void stopHostServer() { if (hostServer != null) hostServer.stop(); }

    public HostResponse dispatchHostRequest(String requestJson) {
        String id = Long.toString(hostRequestCounter.incrementAndGet());
        CompletableFuture<HostResponse> future = new CompletableFuture<>();
        hostResponses.put(id, future);
        runOnUiThread(() -> {
            if (web == null) {
                completeHostResponse(id, 503, "{\"error\":\"Главное приложение закрыто.\"}");
                return;
            }
            String script = "if(window.YarusAndroidHost){YarusAndroidHost.handleNativeRequest(" +
                    JSONObject.quote(id) + "," + JSONObject.quote(requestJson) + ");}" +
                    "else{AndroidFiles.hostRespond(" + JSONObject.quote(id) + ",503,'{\\\"error\\\":\\\"Сервер ещё запускается.\\\"}');}";
            web.evaluateJavascript(script, null);
        });
        try { return future.get(24, TimeUnit.SECONDS); }
        catch (Exception error) { return new HostResponse(503, "{\"error\":\"Главное устройство не ответило вовремя.\"}"); }
        finally { hostResponses.remove(id); }
    }

    public void completeHostResponse(String id, int status, String body) {
        CompletableFuture<HostResponse> future = hostResponses.remove(id);
        if (future != null) future.complete(new HostResponse(status, body == null || body.isEmpty() ? "{}" : body));
    }

    public void reportHostError(String message) {
        runOnUiThread(() -> {
            if (web != null) web.evaluateJavascript("if(typeof toast==='function')toast(" + JSONObject.quote(message) + ",'error');", null);
        });
    }

    public boolean openExternal(String value) {
        try {
            Uri uri = Uri.parse(value == null ? "" : value.trim());
            String scheme = uri.getScheme();
            String host = uri.getHost();
            boolean allowed = "mailto".equals(scheme) && "fixerkrk@yandex.ru".equalsIgnoreCase(uri.getSchemeSpecificPart())
                    || "https".equals(scheme) && "t.me".equalsIgnoreCase(host) && "/bertosh1".equals(uri.getPath());
            if (!allowed) return false;
            Intent intent = new Intent(Intent.ACTION_VIEW, uri);
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            startActivity(intent);
            return true;
        } catch (Exception error) { return false; }
    }

    public void requestStoreReview() {
        // Implemented through the official RuStore SDK in ReviewPrompter; failures stay silent by design.
        ReviewPrompter.launch(this);
    }

    public void beginPortableCreate(String name, String text) {
        pendingPortableText = text;
        Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("application/json");
        intent.putExtra(Intent.EXTRA_TITLE, name);
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        startActivityForResult(intent, PortableWarehouseFile.REQUEST_CREATE);
    }

    public void beginPortableOpen() {
        pendingPortableText = null;
        Intent intent = new Intent(Intent.ACTION_OPEN_DOCUMENT);
        intent.addCategory(Intent.CATEGORY_OPENABLE);
        intent.setType("application/json");
        intent.putExtra(Intent.EXTRA_MIME_TYPES, new String[]{"application/json", "text/json", "text/plain", "application/octet-stream"});
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        startActivityForResult(intent, PortableWarehouseFile.REQUEST_OPEN);
    }

    private void portableCallback(String method, String first, String second) {
        if (web == null) return;
        String script = "if(window.YarusPortable)YarusPortable." + method + "("
                + JSONObject.quote(first == null ? "" : first) + ","
                + JSONObject.quote(second == null ? "" : second) + ");";
        web.evaluateJavascript(script, null);
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
        if (requestCode == PortableWarehouseFile.REQUEST_CREATE || requestCode == PortableWarehouseFile.REQUEST_OPEN) {
            if (resultCode != RESULT_OK || data == null || data.getData() == null) {
                portableFile.cancelPending();
                pendingPortableText = null;
                portableCallback("cancelled", "", "");
                return;
            }
            Uri uri = data.getData();
            int flags = data.getFlags() & (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            try {
                if (requestCode == PortableWarehouseFile.REQUEST_CREATE) {
                    String info = portableFile.attachCreated(uri, flags, pendingPortableText);
                    portableCallback("created", info, "");
                } else {
                    portableFile.stage(uri, flags);
                    portableCallback("opened", portableFile.readPending(), portableFile.pendingName());
                }
            } catch (Exception error) {
                portableFile.cancelPending();
                portableCallback(requestCode == PortableWarehouseFile.REQUEST_CREATE ? "created" : "failed", "", error.getMessage());
            } finally {
                pendingPortableText = null;
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
