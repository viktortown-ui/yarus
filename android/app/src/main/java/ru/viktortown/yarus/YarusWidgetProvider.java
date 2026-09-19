package ru.viktortown.yarus;

import android.app.PendingIntent;
import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.widget.RemoteViews;

/** A local-only home-screen widget. Values refresh whenever the user opens YARUS. */
public class YarusWidgetProvider extends AppWidgetProvider {
    private static final String PREFS = "yarus_widget";

    @Override public void onUpdate(Context context, AppWidgetManager manager, int[] appWidgetIds) {
        SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        for (int id : appWidgetIds) render(context, manager, id, prefs);
    }

    public static void updateAll(Context context, int items, int low, String value, String workspace) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
                .putInt("items", Math.max(0, items))
                .putInt("low", Math.max(0, low))
                .putString("value", clean(value, 40, "—"))
                .putString("workspace", clean(workspace, 60, "Мой склад"))
                .apply();
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        ComponentName component = new ComponentName(context, YarusWidgetProvider.class);
        int[] ids = manager.getAppWidgetIds(component);
        SharedPreferences prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        for (int id : ids) render(context, manager, id, prefs);
    }

    private static String clean(String value, int max, String fallback) {
        String text = value == null ? "" : value.trim().replace('\n', ' ');
        if (text.isEmpty()) return fallback;
        return text.length() > max ? text.substring(0, max - 1) + "…" : text;
    }

    private static void render(Context context, AppWidgetManager manager, int id, SharedPreferences prefs) {
        RemoteViews views = new RemoteViews(context.getPackageName(), R.layout.yarus_widget);
        views.setTextViewText(R.id.widget_workspace, prefs.getString("workspace", "Откройте ЯРУС"));
        views.setTextViewText(R.id.widget_items, Integer.toString(prefs.getInt("items", 0)));
        views.setTextViewText(R.id.widget_low, prefs.getInt("low", 0) + " пополнить");
        views.setTextViewText(R.id.widget_value, prefs.getString("value", "—"));
        Intent intent = new Intent(context, MainActivity.class);
        intent.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent open = PendingIntent.getActivity(context, 100, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        views.setOnClickPendingIntent(R.id.widget_root, open);
        manager.updateAppWidget(id, views);
    }
}
