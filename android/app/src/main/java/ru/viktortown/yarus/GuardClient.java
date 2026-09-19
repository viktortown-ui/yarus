package ru.viktortown.yarus;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.webkit.WebResourceRequest;
public class GuardClient extends WebViewClient {
    @Override public boolean shouldOverrideUrlLoading(WebView view, String url) { return true; }
    @Override public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) { return true; }
}
