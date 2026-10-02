package ru.viktortown.yarus;

import android.Manifest;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.Path;
import android.graphics.Rect;
import android.graphics.RectF;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.media.Image;
import android.os.Build;
import android.os.Bundle;
import android.os.SystemClock;
import android.util.Size;
import android.view.Gravity;
import android.view.HapticFeedbackConstants;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowInsets;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;

import androidx.activity.ComponentActivity;
import androidx.annotation.NonNull;
import androidx.annotation.OptIn;
import androidx.camera.core.CameraSelector;
import androidx.camera.core.ExperimentalGetImage;
import androidx.camera.core.ImageProxy;
import androidx.camera.core.TorchState;
import androidx.camera.core.ZoomState;
import androidx.camera.view.CameraController;
import androidx.camera.view.LifecycleCameraController;
import androidx.camera.view.PreviewView;
import androidx.core.content.ContextCompat;

import com.google.mlkit.vision.barcode.BarcodeScanner;
import com.google.mlkit.vision.barcode.BarcodeScannerOptions;
import com.google.mlkit.vision.barcode.BarcodeScanning;
import com.google.mlkit.vision.barcode.ZoomSuggestionOptions;
import com.google.mlkit.vision.barcode.common.Barcode;
import com.google.mlkit.vision.common.InputImage;

import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Full-screen, on-device barcode scanner for Android phone and tablet builds. */
@OptIn(markerClass = ExperimentalGetImage.class)
public final class ScannerActivity extends ComponentActivity {
    public static final String EXTRA_VALUE = "ru.viktortown.yarus.scan.VALUE";
    public static final String EXTRA_FORMAT = "ru.viktortown.yarus.scan.FORMAT";
    public static final String EXTRA_ERROR = "ru.viktortown.yarus.scan.ERROR";
    private static final int CAMERA_PERMISSION = 2001;
    private static final int BRAND = Color.rgb(29, 100, 83);
    private static final int MINT = Color.rgb(181, 236, 212);

    private final BarcodeScanPolicy policy = new BarcodeScanPolicy();
    private final ExecutorService analyzerExecutor = Executors.newSingleThreadExecutor();
    private PreviewView preview;
    private LifecycleCameraController controller;
    private BarcodeScanner scanner;
    private TextView status;
    private TextView code;
    private Button torch;
    private Button use;
    private Button repeat;
    private LinearLayout topPanel;
    private LinearLayout bottomPanel;
    private volatile boolean paused;
    private volatile boolean finished;
    private boolean torchOn;
    private String pendingValue = "";
    private String pendingFormat = "";
    private long lastCloserHintAt;

    @Override protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().setStatusBarColor(Color.rgb(7, 17, 15));
        getWindow().setNavigationBarColor(Color.rgb(7, 17, 15));
        if (Build.VERSION.SDK_INT >= 35) getWindow().setNavigationBarContrastEnforced(false);
        buildInterface();
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA)
                == PackageManager.PERMISSION_GRANTED) startCamera();
        else requestPermissions(new String[]{Manifest.permission.CAMERA}, CAMERA_PERMISSION);
    }

    private void buildInterface() {
        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.rgb(7, 17, 15));
        preview = new PreviewView(this);
        preview.setScaleType(PreviewView.ScaleType.FILL_CENTER);
        preview.setImplementationMode(PreviewView.ImplementationMode.COMPATIBLE);
        preview.setContentDescription("Изображение с задней камеры. Коснитесь кода для фокусировки.");
        root.addView(preview, match());
        root.addView(new GuideView(), match());

        topPanel = new LinearLayout(this);
        topPanel.setOrientation(LinearLayout.HORIZONTAL);
        topPanel.setGravity(Gravity.CENTER_VERTICAL);
        topPanel.setPadding(dp(12), dp(12), dp(12), dp(10));
        topPanel.setBackgroundColor(Color.argb(176, 7, 17, 15));
        Button close = button("Закрыть", false);
        close.setContentDescription("Закрыть сканер");
        close.setOnClickListener(view -> cancel());
        topPanel.addView(close, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(48)));
        LinearLayout titleBox = new LinearLayout(this);
        titleBox.setOrientation(LinearLayout.VERTICAL);
        titleBox.setPadding(dp(14), 0, 0, 0);
        TextView title = text("Сканер", 20, Color.WHITE, true);
        TextView subtitle = text("Один код — внутри рамки", 13, Color.rgb(215, 231, 224), false);
        titleBox.addView(title);
        titleBox.addView(subtitle);
        topPanel.addView(titleBox, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        FrameLayout.LayoutParams topParams = new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.TOP);
        root.addView(topPanel, topParams);

        bottomPanel = new LinearLayout(this);
        bottomPanel.setOrientation(LinearLayout.VERTICAL);
        bottomPanel.setGravity(Gravity.CENTER_HORIZONTAL);
        bottomPanel.setPadding(dp(16), dp(15), dp(16), dp(16));
        GradientDrawable bottomBackground = new GradientDrawable();
        bottomBackground.setColor(Color.argb(224, 7, 17, 15));
        bottomBackground.setCornerRadii(new float[]{dp(22),dp(22),dp(22),dp(22),0,0,0,0});
        bottomPanel.setBackground(bottomBackground);
        code = text("", 22, Color.WHITE, true);
        code.setTypeface(Typeface.MONOSPACE, Typeface.BOLD);
        code.setGravity(Gravity.CENTER);
        code.setVisibility(View.GONE);
        code.setTextIsSelectable(true);
        bottomPanel.addView(code, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        status = text("Включаем камеру…", 15, Color.WHITE, false);
        status.setGravity(Gravity.CENTER);
        status.setPadding(0, dp(4), 0, dp(12));
        status.setAccessibilityLiveRegion(View.ACCESSIBILITY_LIVE_REGION_POLITE);
        bottomPanel.addView(status, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        LinearLayout controls = new LinearLayout(this);
        controls.setGravity(Gravity.CENTER);
        controls.setOrientation(LinearLayout.HORIZONTAL);
        torch = button("Фонарик", false);
        torch.setVisibility(View.GONE);
        torch.setOnClickListener(view -> toggleTorch());
        use = button("Использовать", true);
        use.setVisibility(View.GONE);
        use.setOnClickListener(view -> finishResult(pendingValue, pendingFormat));
        repeat = button("Повторить", false);
        repeat.setVisibility(View.GONE);
        repeat.setOnClickListener(view -> resumeScanning());
        controls.addView(torch, weightedButton());
        controls.addView(use, weightedButton());
        controls.addView(repeat, weightedButton());
        bottomPanel.addView(controls, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        TextView help = text("Коснитесь кода для фокуса · разведите пальцы для увеличения", 12, Color.rgb(181, 205, 194), false);
        help.setGravity(Gravity.CENTER);
        help.setPadding(0, dp(10), 0, 0);
        bottomPanel.addView(help, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        FrameLayout.LayoutParams bottomParams = new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.BOTTOM);
        root.addView(bottomPanel, bottomParams);
        setContentView(root);

        root.setOnApplyWindowInsetsListener((view, insets) -> {
            int top;
            int bottom;
            int left;
            int right;
            if (Build.VERSION.SDK_INT >= 30) {
                android.graphics.Insets bars = insets.getInsets(WindowInsets.Type.systemBars());
                top = bars.top; bottom = bars.bottom; left = bars.left; right = bars.right;
            } else {
                top = insets.getSystemWindowInsetTop(); bottom = insets.getSystemWindowInsetBottom();
                left = insets.getSystemWindowInsetLeft(); right = insets.getSystemWindowInsetRight();
            }
            topPanel.setPadding(dp(12) + left, dp(12) + top, dp(12) + right, dp(10));
            bottomPanel.setPadding(dp(16) + left, dp(15), dp(16) + right, dp(16) + bottom);
            return insets;
        });
    }

    private void startCamera() {
        try {
            controller = new LifecycleCameraController(this);
            controller.setCameraSelector(CameraSelector.DEFAULT_BACK_CAMERA);
            controller.setEnabledUseCases(CameraController.IMAGE_ANALYSIS);
            controller.setTapToFocusEnabled(true);
            controller.setPinchToZoomEnabled(true);
            controller.setImageAnalysisTargetSize(new CameraController.OutputSize(new Size(1280, 720)));
            controller.bindToLifecycle(this);
            preview.setController(controller);
            float maximumZoom = 4.0f;
            ZoomState zoomState = controller.getZoomState().getValue();
            if (zoomState != null) maximumZoom = Math.min(4.0f, zoomState.getMaxZoomRatio());
            final float supportedZoom = Math.max(1.0f, maximumZoom);
            ZoomSuggestionOptions zoomOptions = new ZoomSuggestionOptions.Builder(zoomRatio -> {
                if (finished || controller == null) return false;
                ZoomState current = controller.getZoomState().getValue();
                float minimum = current == null ? 1.0f : current.getMinZoomRatio();
                float maximum = current == null ? supportedZoom : Math.min(supportedZoom, current.getMaxZoomRatio());
                controller.setZoomRatio(Math.max(minimum, Math.min(maximum, zoomRatio)));
                return true;
            }).setMaxSupportedZoomRatio(supportedZoom).build();
            BarcodeScannerOptions options = new BarcodeScannerOptions.Builder()
                    .setBarcodeFormats(Barcode.FORMAT_QR_CODE, Barcode.FORMAT_EAN_13,
                            Barcode.FORMAT_EAN_8, Barcode.FORMAT_CODE_128)
                    .enableAllPotentialBarcodes()
                    .setZoomSuggestionOptions(zoomOptions)
                    .build();
            scanner = BarcodeScanning.getClient(options);
            controller.setImageAnalysisAnalyzer(analyzerExecutor, this::analyze);
            controller.getTorchState().observe(this, state -> {
                torchOn = state != null && state == TorchState.ON;
                torch.setText(torchOn ? "Выключить свет" : "Фонарик");
            });
            torch.setVisibility(View.VISIBLE);
            setStatus("Наведите код в рамку и держите телефон спокойно.");
        } catch (Exception error) {
            fail("Не удалось открыть заднюю камеру. Перезапустите ЯРУС или обновите приложение.");
        }
    }

    private void analyze(@NonNull ImageProxy proxy) {
        BarcodeScanner activeScanner = scanner;
        if (paused || finished || activeScanner == null) { proxy.close(); return; }
        Image mediaImage = proxy.getImage();
        if (mediaImage == null) { proxy.close(); return; }
        int rotation = proxy.getImageInfo().getRotationDegrees();
        int width = rotation % 180 == 0 ? proxy.getWidth() : proxy.getHeight();
        int height = rotation % 180 == 0 ? proxy.getHeight() : proxy.getWidth();
        InputImage image = InputImage.fromMediaImage(mediaImage, rotation);
        try {
            activeScanner.process(image)
                    // ML Kit may finish a frame after the activity has started closing. Keep
                    // its callbacks off analyzerExecutor: that executor is stopped in
                    // onDestroy and would otherwise reject the late callback on the main
                    // thread, crashing the whole application while leaving the scanner.
                    .addOnSuccessListener(ContextCompat.getMainExecutor(this),
                            barcodes -> inspect(barcodes, width, height))
                    .addOnFailureListener(ContextCompat.getMainExecutor(this),
                            error -> setStatus("Не удаётся распознать код. Наведите резкость касанием."))
                    .addOnCompleteListener(ContextCompat.getMainExecutor(this),
                            ignored -> proxy.close());
        } catch (RuntimeException error) {
            proxy.close();
            if (!finished) setStatus("Не удаётся обработать кадр. Повторите сканирование.");
        }
    }

    private void inspect(List<Barcode> barcodes, int width, int height) {
        if (paused || finished) return;
        Barcode best = null;
        String bestFormat = null;
        double bestDistance = Double.MAX_VALUE;
        boolean needsCloser = false;
        for (Barcode barcode : barcodes) {
            String value = BarcodeScanPolicy.clean(barcode.getRawValue());
            String format = formatName(barcode.getFormat());
            Rect bounds = barcode.getBoundingBox();
            if (value == null || format == null || bounds == null) { if (bounds != null) needsCloser = true; continue; }
            float centerX = bounds.exactCenterX(), centerY = bounds.exactCenterY();
            if (centerX < width * .08f || centerX > width * .92f || centerY < height * .10f || centerY > height * .90f) continue;
            int longSide = Math.max(bounds.width(), bounds.height());
            int shortSide = Math.min(bounds.width(), bounds.height());
            boolean largeEnough = "QR".equals(format) ? shortSide >= 90 : longSide >= 180;
            if (!largeEnough) { needsCloser = true; continue; }
            double distance = Math.hypot(centerX - width / 2.0, centerY - height / 2.0);
            if (distance < bestDistance) { best = barcode; bestFormat = format; bestDistance = distance; }
        }
        if (best == null) {
            if (needsCloser && SystemClock.elapsedRealtime() - lastCloserHintAt > 1200) {
                lastCloserHintAt = SystemClock.elapsedRealtime();
                setStatus("Поднесите код немного ближе и коснитесь его для фокуса.");
            }
            return;
        }
        BarcodeScanPolicy.Result stable = policy.observe(best.getRawValue(), bestFormat, SystemClock.elapsedRealtime());
        if (stable == null) { setStatus("Код найден. Удерживайте камеру ещё секунду…"); return; }
        paused = true;
        controller.clearImageAnalysisAnalyzer();
        runOnUiThread(() -> {
            if (finished) return;
            preview.performHapticFeedback(Build.VERSION.SDK_INT >= 30
                    ? HapticFeedbackConstants.CONFIRM : HapticFeedbackConstants.LONG_PRESS);
            if ("QR".equals(stable.format)) finishResult(stable.value, stable.format);
            else showConfirmation(stable.value, stable.format);
        });
    }

    private void showConfirmation(String value, String format) {
        pendingValue = value;
        pendingFormat = format;
        code.setText(value);
        code.setContentDescription("Считанный код: " + value);
        code.setVisibility(View.VISIBLE);
        status.setText("Сверьте цифры с упаковкой.");
        torch.setVisibility(View.GONE);
        use.setVisibility(View.VISIBLE);
        repeat.setVisibility(View.VISIBLE);
        use.requestFocus();
    }

    private void resumeScanning() {
        if (controller == null || finished) return;
        pendingValue = "";
        pendingFormat = "";
        policy.reset();
        code.setVisibility(View.GONE);
        use.setVisibility(View.GONE);
        repeat.setVisibility(View.GONE);
        torch.setVisibility(View.VISIBLE);
        paused = false;
        setStatus("Наведите код в рамку и держите телефон спокойно.");
        controller.setImageAnalysisAnalyzer(analyzerExecutor, this::analyze);
    }

    private void toggleTorch() {
        if (controller == null) return;
        controller.enableTorch(!torchOn);
    }

    private static String formatName(int format) {
        switch (format) {
            case Barcode.FORMAT_QR_CODE: return "QR";
            case Barcode.FORMAT_EAN_13: return "EAN-13";
            case Barcode.FORMAT_EAN_8: return "EAN-8";
            case Barcode.FORMAT_CODE_128: return "Code 128";
            default: return null;
        }
    }

    private void finishResult(String value, String format) {
        if (finished) return;
        finished = true;
        Intent result = new Intent();
        result.putExtra(EXTRA_VALUE, value);
        result.putExtra(EXTRA_FORMAT, format);
        setResult(RESULT_OK, result);
        finish();
    }

    private void cancel() {
        if (finished) return;
        finished = true;
        setResult(RESULT_CANCELED);
        finish();
    }

    private void fail(String message) {
        if (finished) return;
        finished = true;
        Intent result = new Intent();
        result.putExtra(EXTRA_ERROR, message);
        setResult(RESULT_CANCELED, result);
        finish();
    }

    private void setStatus(String message) {
        runOnUiThread(() -> { if (!finished && status != null) status.setText(message); });
    }

    @Override public void onRequestPermissionsResult(int requestCode, @NonNull String[] permissions, @NonNull int[] results) {
        super.onRequestPermissionsResult(requestCode, permissions, results);
        if (requestCode != CAMERA_PERMISSION) return;
        if (results.length > 0 && results[0] == PackageManager.PERMISSION_GRANTED) startCamera();
        else fail("Нет доступа к камере. Разрешите камеру для ЯРУС в настройках телефона.");
    }

    @Override protected void onDestroy() {
        finished = true;
        paused = true;
        if (controller != null) {
            controller.clearImageAnalysisAnalyzer();
            controller = null;
        }
        if (scanner != null) {
            scanner.close();
            scanner = null;
        }
        analyzerExecutor.shutdownNow();
        super.onDestroy();
    }

    private Button button(String label, boolean primary) {
        Button button = new Button(this);
        button.setText(label);
        button.setAllCaps(false);
        button.setTextSize(14);
        button.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        button.setTextColor(primary ? Color.WHITE : MINT);
        button.setMinHeight(dp(48));
        button.setPadding(dp(14), 0, dp(14), 0);
        GradientDrawable background = new GradientDrawable();
        background.setColor(primary ? BRAND : Color.argb(70, 181, 236, 212));
        background.setCornerRadius(dp(12));
        background.setStroke(dp(1), primary ? BRAND : Color.argb(130, 181, 236, 212));
        button.setBackground(background);
        return button;
    }

    private TextView text(String value, int size, int color, boolean bold) {
        TextView text = new TextView(this);
        text.setText(value);
        text.setTextSize(size);
        text.setTextColor(color);
        if (bold) text.setTypeface(Typeface.DEFAULT, Typeface.BOLD);
        return text;
    }

    private LinearLayout.LayoutParams weightedButton() {
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(0, dp(50), 1);
        params.setMargins(dp(4), 0, dp(4), 0);
        return params;
    }

    private FrameLayout.LayoutParams match() {
        return new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT);
    }

    private int dp(float value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private final class GuideView extends View {
        private final Paint dim = new Paint(Paint.ANTI_ALIAS_FLAG);
        private final Paint border = new Paint(Paint.ANTI_ALIAS_FLAG);
        GuideView() {
            super(ScannerActivity.this);
            dim.setColor(Color.argb(78, 0, 0, 0));
            border.setColor(MINT);
            border.setStyle(Paint.Style.STROKE);
            border.setStrokeWidth(dp(2));
            setImportantForAccessibility(IMPORTANT_FOR_ACCESSIBILITY_NO);
        }
        @Override protected void onDraw(Canvas canvas) {
            super.onDraw(canvas);
            float width = getWidth() * .82f;
            float height = Math.min(width * .48f, getHeight() * .27f);
            RectF guide = new RectF((getWidth() - width) / 2f, (getHeight() - height) / 2f,
                    (getWidth() + width) / 2f, (getHeight() + height) / 2f);
            Path outside = new Path();
            outside.setFillType(Path.FillType.EVEN_ODD);
            outside.addRect(0, 0, getWidth(), getHeight(), Path.Direction.CW);
            outside.addRoundRect(guide, dp(16), dp(16), Path.Direction.CW);
            canvas.drawPath(outside, dim);
            canvas.drawRoundRect(guide, dp(16), dp(16), border);
        }
    }
}
