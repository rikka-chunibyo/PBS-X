package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.widget.LinearLayout;
import android.widget.TextView;
import java.util.concurrent.ExecutorService;
import java.util.function.Consumer;
import org.json.JSONArray;

/** Asynchronous directory-only picker. Closing it never retains a privileged reader. */
final class ShizukuFolderPicker {
  private final Activity activity;
  private final ExecutorService executor;
  private final Consumer<String> selected;
  private final AlertDialog dialog;
  private final TextView location;
  private final LinearLayout entries;
  private String path = ".";
  private boolean closed;
  private int generation;

  ShizukuFolderPicker(Activity activity, ExecutorService executor, Consumer<String> selected) {
    this.activity = activity;
    this.executor = executor;
    this.selected = selected;
    LinearLayout form = Ui.form(activity);
    location = Ui.hint(form, "Internal shared storage");
    entries = Ui.column(activity);
    android.widget.ScrollView scroll = new android.widget.ScrollView(activity);
    scroll.addView(entries);
    form.addView(scroll, new LinearLayout.LayoutParams(-1, Ui.dp(activity, 300)));
    dialog =
        new AlertDialog.Builder(activity)
            .setTitle("Folder via Shizuku")
            .setView(form)
            .setNegativeButton("Cancel", null)
            .setNeutralButton("Up", null)
            .setPositiveButton("Use this folder", null)
            .create();
    dialog.setOnDismissListener(d -> closed = true);
    dialog.setOnShowListener(
        d -> {
          dialog
              .getButton(AlertDialog.BUTTON_NEUTRAL)
              .setOnClickListener(
                  v -> {
                    int slash = path.lastIndexOf('/');
                    load(slash < 0 ? "." : path.substring(0, slash));
                  });
          dialog
              .getButton(AlertDialog.BUTTON_POSITIVE)
              .setOnClickListener(
                  v -> {
                    dialog.dismiss();
                    selected.accept(path);
                  });
          load(".");
        });
  }

  void show() {
    dialog.show();
  }

  private void load(String next) {
    int request = ++generation;
    entries.removeAllViews();
    Ui.hint(entries, "Reading folders…");
    location.setText(next.equals(".") ? "Internal shared storage" : next);
    dialog.getButton(AlertDialog.BUTTON_POSITIVE).setEnabled(false);
    dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setEnabled(false);
    executor.execute(
        () -> {
          JSONArray folders = null;
          Exception failure = null;
          try (ShizukuBridge bridge = new ShizukuBridge(activity)) {
            folders = new JSONArray(bridge.files().folders(next));
          } catch (Exception e) {
            failure = e;
          }
          JSONArray result = folders;
          Exception error = failure;
          activity.runOnUiThread(
              () -> {
                if (closed
                    || activity.isDestroyed()
                    || activity.isFinishing()
                    || request != generation) return;
                entries.removeAllViews();
                // Keep the last successful location so Up remains a safe recovery path on failure.
                dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setEnabled(!path.equals("."));
                if (error != null) {
                  Ui.hint(
                      entries,
                      error.getMessage() == null ? "Could not read folders" : error.getMessage());
                  Ui.button(entries, "Retry", false, v -> load(next));
                  Ui.button(entries, "Internal storage", false, v -> load("."));
                  return;
                }
                path = next;
                dialog.getButton(AlertDialog.BUTTON_POSITIVE).setEnabled(!path.equals("."));
                dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setEnabled(!path.equals("."));
                if (result.length() == 0)
                  Ui.hint(entries, "No subfolders. You can select this folder.");
                for (int i = 0; i < result.length(); i++) {
                  String name = result.optJSONObject(i).optString("name");
                  String child = next.equals(".") ? name : next + "/" + name;
                  Ui.button(entries, name + "  ›", false, v -> load(child));
                }
              });
        });
  }
}
