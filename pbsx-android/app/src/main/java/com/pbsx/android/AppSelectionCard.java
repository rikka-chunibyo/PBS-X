package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.view.View;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.LinearLayout;
import android.widget.TextView;
import java.util.concurrent.ExecutorService;
import org.json.JSONArray;

/** Independent app filter for one source; changing APK choices never changes data/OBB choices. */
final class AppSelectionCard {
  private final Activity activity;
  private final ExecutorService executor;
  private final BackupJob job;
  private final String kind, label;
  private final CheckBox enabled, all;
  private final LinearLayout details;
  private final TextView summary;
  private final Button choose;
  private boolean master, loading;

  AppSelectionCard(
      Activity activity,
      LinearLayout parent,
      BackupJob job,
      String kind,
      String label,
      ExecutorService executor)
      throws Exception {
    this.activity = activity;
    this.executor = executor;
    this.job = job;
    this.kind = kind;
    this.label = kind.equals("apks") ? "APKs" : label;
    LinearLayout card = Ui.card(parent);
    enabled =
        Ui.check(
            card, label, kind.equals("apks") ? job.apks : kind.equals("data") ? job.data : job.obb);
    Ui.hint(
        card,
        kind.equals("apks")
            ? "APK installers only; excludes app-private databases and account logins."
            : "External app files only. Android may deny access to some app folders.");
    details = Ui.column(activity);
    card.addView(details);
    all =
        Ui.check(
            details,
            kind.equals("apks") ? "All APKs" : "All " + label + " folders",
            job.appAll(kind));
    summary = Ui.hint(details, "");
    choose = Ui.button(details, "Choose apps · " + this.label, false, v -> pick());
    enabled.setOnCheckedChangeListener(
        (button, checked) -> {
          if (kind.equals("apks")) job.apks = checked;
          else if (kind.equals("data")) job.data = checked;
          else job.obb = checked;
          setEnabled(master);
        });
    all.setOnCheckedChangeListener(
        (button, checked) -> {
          try {
            save();
            refresh();
          } catch (Exception e) {
            error(e);
          }
        });
    refresh();
    setEnabled(false);
  }

  void save() throws Exception {
    job.appSelection(kind, all.isChecked(), job.appPackages(kind));
  }

  void setEnabled(boolean master) {
    this.master = master;
    enabled.setEnabled(master);
    enabled.setAlpha(master ? 1f : .4f);
    details.setVisibility(enabled.isChecked() ? View.VISIBLE : View.GONE);
    boolean active = master && enabled.isChecked();
    all.setEnabled(active);
    choose.setEnabled(active && !loading);
    details.setAlpha(active ? 1f : .4f);
  }

  private void refresh() throws Exception {
    summary.setText(
        all.isChecked()
            ? "All eligible apps for this source"
            : "Selected " + label + ": " + job.appPackages(kind).length());
  }

  private void pick() {
    if (loading) return;
    final JSONArray selected;
    try {
      selected = new JSONArray(job.appPackages(kind).toString());
    } catch (Exception e) {
      error(e);
      return;
    }
    loading = true;
    setEnabled(master);
    executor.execute(
        () -> {
          try (ShizukuBridge bridge = new ShizukuBridge(activity)) {
            JSONArray inventory = new JSONArray(bridge.files().apps());
            if (inventory.length() > 500)
              throw new Exception("App picker supports at most 500 packages");
            String[] packages = new String[inventory.length()];
            String[] labels = new String[inventory.length()];
            boolean[] checked = new boolean[inventory.length()];
            for (int i = 0; i < packages.length; i++) {
              packages[i] = inventory.getString(i);
              labels[i] = packages[i];
              try {
                android.content.pm.PackageManager pm = activity.getPackageManager();
                labels[i] =
                    pm.getApplicationLabel(pm.getApplicationInfo(packages[i], 0))
                        + "\n"
                        + packages[i];
              } catch (Exception ignored) {
              }
              for (int j = 0; j < selected.length(); j++)
                if (packages[i].equals(selected.getString(j))) checked[i] = true;
            }
            activity.runOnUiThread(
                () -> {
                  if (activity.isDestroyed() || activity.isFinishing()) return;
                  loading = false;
                  setEnabled(master);
                  if (!master || !enabled.isChecked()) return;
                  new AlertDialog.Builder(activity)
                      .setTitle("Choose " + label)
                      .setMultiChoiceItems(
                          labels, checked, (dialog, which, value) -> checked[which] = value)
                      .setNegativeButton("Cancel", null)
                      .setPositiveButton(
                          "Use selected",
                          (dialog, which) -> {
                            try {
                              JSONArray apps = new JSONArray();
                              for (int i = 0; i < checked.length; i++)
                                if (checked[i]) apps.put(packages[i]);
                              job.appSelection(kind, false, apps);
                              all.setChecked(false);
                              refresh();
                            } catch (Exception e) {
                              error(e);
                            }
                          })
                      .show();
                });
          } catch (Exception e) {
            activity.runOnUiThread(
                () -> {
                  if (activity.isDestroyed() || activity.isFinishing()) return;
                  loading = false;
                  setEnabled(master);
                  error(e);
                });
          }
        });
  }

  private void error(Exception e) {
    new AlertDialog.Builder(activity)
        .setMessage(e.getMessage() == null ? "Could not update app selection" : e.getMessage())
        .setPositiveButton("OK", null)
        .show();
  }
}
