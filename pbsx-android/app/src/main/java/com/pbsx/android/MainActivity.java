package com.pbsx.android;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.View;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import com.pbsx.core.mobilecore.Mobilecore;
import java.text.DateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.json.JSONArray;
import org.json.JSONObject;
import rikka.shizuku.Shizuku;

public final class MainActivity extends Activity {
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private final Handler handler = new Handler(Looper.getMainLooper());
  private TextView state, connection, sources;
  private String restoreHost = "";
  private long restoreStamp;
  private final Runnable refresh =
      new Runnable() {
        public void run() {
          if (state != null)
            state.setText(
                Settings.prefs(MainActivity.this)
                    .getString("status", "Ready. Choose folders, then start a backup."));
          handler.postDelayed(this, 1500);
        }
      };
  private final Shizuku.OnRequestPermissionResultListener permission =
      (code, result) ->
          runOnUiThread(
              () ->
                  message(
                      result == PackageManager.PERMISSION_GRANTED
                          ? "Shizuku permission granted. Enable the desired sources in Settings."
                          : "Shizuku permission denied. Ordinary folder backups remain"
                              + " available."));

  @Override
  public void onCreate(Bundle saved) {
    super.onCreate(saved);
    if (saved != null) {
      restoreHost = saved.getString("restoreHost", "");
      restoreStamp = saved.getLong("restoreStamp", 0);
    }
    Shizuku.addRequestPermissionResultListener(permission);
    ScrollView scroll = new ScrollView(this);
    LinearLayout page = new LinearLayout(this);
    page.setOrientation(LinearLayout.VERTICAL);
    page.setPadding(28, 38, 28, 32);
    scroll.setFillViewport(true);
    scroll.addView(page);
    setContentView(scroll);
    // Keep controls within system bars on Android 15+ edge-to-edge layouts.
    if (Build.VERSION.SDK_INT >= 30)
      page.setOnApplyWindowInsetsListener(
          (v, insets) -> {
            android.graphics.Insets bars =
                insets.getInsets(android.view.WindowInsets.Type.systemBars());
            v.setPadding(28, 24 + bars.top, 28, 24 + bars.bottom);
            return insets;
          });
    title(page, "PBS-X Backup");
    text(page, "Android preview · Files, media and optional Shizuku sources");
    connection = text(page, "");
    button(page, "Enroll with PBS-X code", v -> enroll());
    button(page, "Use an existing API key", v -> manualKey());
    button(page, "Change storage API key", v -> storageKey());
    title(page, "Backup folders");
    sources = text(page, "");
    button(page, "Add a folder", v -> pick(100));
    button(page, "Remove folders", v -> removeSources());
    text(
        page,
        "Pick DCIM for photos and videos, then add other folders. Every run includes all selected"
            + " sources in a complete restore point.");
    button(
        page,
        "Back up now",
        v -> {
          if (Operations.running()) {
            message("An operation is already running");
            return;
          }
          startOperation(new Intent(this, BackupService.class));
        });
    button(page, "Browse snapshots and restore", v -> snapshots());
    button(page, "Settings", v -> settings());
    button(page, "Cancel current operation", v -> Operations.cancel());
    title(page, "Latest activity");
    state = text(page, "");
    text(
        page,
        "Restore creates a new folder. APK backups contain installation files, not private app"
            + " data. This preview does not make a full phone image.");
    updateSummary();
  }

  @Override
  protected void onResume() {
    super.onResume();
    handler.post(refresh);
    updateSummary();
  }

  @Override
  protected void onPause() {
    handler.removeCallbacks(refresh);
    super.onPause();
  }

  @Override
  protected void onDestroy() {
    Shizuku.removeRequestPermissionResultListener(permission);
    executor.shutdown();
    super.onDestroy();
  }

  @Override
  protected void onSaveInstanceState(Bundle out) {
    out.putString("restoreHost", restoreHost);
    out.putLong("restoreStamp", restoreStamp);
    super.onSaveInstanceState(out);
  }

  private TextView text(LinearLayout parent, String value) {
    TextView t = new TextView(this);
    t.setText(value);
    t.setTextSize(15);
    t.setPadding(0, 10, 0, 14);
    parent.addView(t);
    return t;
  }

  private void title(LinearLayout p, String value) {
    TextView t = text(p, value);
    t.setTextSize(25);
    t.setTextColor(Color.rgb(34, 54, 105));
  }

  private void button(LinearLayout p, String label, View.OnClickListener click) {
    Button b = new Button(this);
    b.setText(label);
    b.setAllCaps(false);
    p.addView(b);
    b.setOnClickListener(click);
  }

  private LinearLayout form() {
    LinearLayout f = new LinearLayout(this);
    f.setPadding(30, 12, 30, 4);
    f.setOrientation(LinearLayout.VERTICAL);
    return f;
  }

  private EditText input(LinearLayout f, String hint, String value, boolean secret) {
    EditText e = new EditText(this);
    e.setHint(hint);
    e.setText(value);
    e.setSingleLine(true);
    e.setInputType(
        InputType.TYPE_CLASS_TEXT
            | (secret
                ? InputType.TYPE_TEXT_VARIATION_PASSWORD
                : InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS));
    f.addView(e);
    return e;
  }

  private CheckBox check(LinearLayout f, String label, boolean enabled) {
    CheckBox b = new CheckBox(this);
    b.setText(label);
    b.setChecked(enabled);
    f.addView(b);
    return b;
  }

  private void message(String text) {
    if (!isFinishing() && !isDestroyed())
      new AlertDialog.Builder(this).setMessage(text).setPositiveButton("OK", null).show();
  }

  private void error(Exception e) {
    message(e.getMessage() == null ? "Operation failed" : e.getMessage());
  }

  private void background(String label, Task task) {
    Settings.status(this, label);
    executor.execute(
        () -> {
          try {
            String result = task.run();
            runOnUiThread(
                () -> {
                  Settings.status(this, result);
                  updateSummary();
                });
          } catch (Exception e) {
            runOnUiThread(
                () -> {
                  Settings.status(this, "Failed: " + e.getMessage());
                  error(e);
                });
          }
        });
  }

  private interface Task {
    String run() throws Exception;
  }

  private void updateSummary() {
    try {
      String config = Vault.load(this);
      connection.setText(
          config.isEmpty()
              ? "No connection configured"
              : new JSONObject(config).optString("server")
                  + "\nHost: "
                  + new JSONObject(config).optString("host_id"));
      JSONArray list = Settings.sources(this);
      ArrayList<String> names = new ArrayList<>();
      for (int i = 0; i < list.length(); i++) names.add(list.getJSONObject(i).getString("label"));
      SharedPreferences p = Settings.prefs(this);
      if (p.getBoolean("shizuku", false)) {
        if (p.getBoolean("apks", false)) names.add("Installed APKs (Shizuku)");
        if (p.getBoolean("data", false)) names.add("Android/data (Shizuku)");
        if (p.getBoolean("obb", false)) names.add("Android/obb (Shizuku)");
      }
      sources.setText(names.isEmpty() ? "No folders selected" : String.join("\n", names));
    } catch (Exception e) {
      connection.setText("Connection unavailable: " + e.getMessage());
    }
  }

  private boolean idle() {
    if (Operations.running()) {
      message("Wait for the current operation or cancel it before changing settings");
      return false;
    }
    return true;
  }

  private void enroll() {
    if (!idle()) return;
    LinearLayout f = form();
    text(
        f,
        "Create a host and enrollment code in PBS-X → Hosts. Its backup job must allow the"
            + " datastore and namespace selected in Settings.");
    EditText code = input(f, "PBS-X enrollment code", "", true);
    new AlertDialog.Builder(this)
        .setTitle("Automatic enrollment")
        .setView(f)
        .setNegativeButton("Cancel", null)
        .setPositiveButton(
            "Enroll",
            (d, w) -> {
              String value = code.getText().toString().trim();
              code.setText("");
              background(
                  "Enrolling…",
                  () -> {
                    String cfg = Mobilecore.enroll(value, Build.MANUFACTURER + " " + Build.MODEL);
                    Vault.save(this, cfg);
                    return "Enrolled. Select backup folders and verify datastore Settings.";
                  });
            })
        .show();
  }

  private void manualKey() {
    if (!idle()) return;
    LinearLayout f = form();
    text(
        f,
        "Use an existing PBS API token. Enter its full auth ID (user@realm!token), secret, and"
            + " server SHA-256 certificate fingerprint. The host ID becomes the backup group.");
    EditText server = input(f, "https://server:8007", "", false),
        host = input(f, "Host ID", "", false),
        pin = input(f, "SHA-256 certificate fingerprint", "", false),
        id = input(f, "API token auth ID", "", false),
        secret = input(f, "API token secret", "", true);
    new AlertDialog.Builder(this)
        .setTitle("Existing API key")
        .setView(f)
        .setNegativeButton("Cancel", null)
        .setPositiveButton(
            "Save",
            (d, w) -> {
              try {
                JSONObject cfg =
                    new JSONObject()
                        .put("server", server.getText().toString().trim())
                        .put("host_id", host.getText().toString().trim())
                        .put("fingerprint", pin.getText().toString().trim())
                        .put("auth_id", id.getText().toString().trim())
                        .put("secret", secret.getText().toString().trim());
                Mobilecore.newClient(cfg.toString());
                Vault.save(this, cfg.toString());
                secret.setText("");
                updateSummary();
              } catch (Exception e) {
                error(e);
              }
            })
        .show();
  }

  private void storageKey() {
    if (!idle()) return;
    try {
      String raw = Vault.load(this);
      if (raw.isEmpty()) {
        message("Enroll or configure a connection first");
        return;
      }
      JSONObject cfg = new JSONObject(raw);
      LinearLayout f = form();
      text(
          f,
          "Optional storage key for broader backup/restore access. Leave both fields empty to use"
              + " this host’s automatic key. Enrollment identity is retained.");
      EditText id = input(f, "Storage API token auth ID", cfg.optString("storage_auth_id"), false),
          secret = input(f, "Storage API token secret", "", true);
      new AlertDialog.Builder(this)
          .setTitle("Storage credentials")
          .setView(f)
          .setNegativeButton("Cancel", null)
          .setPositiveButton(
              "Save",
              (d, w) -> {
                try {
                  cfg.put("storage_auth_id", id.getText().toString().trim())
                      .put("storage_secret", secret.getText().toString().trim());
                  Mobilecore.newClient(cfg.toString());
                  Vault.save(this, cfg.toString());
                  secret.setText("");
                  message("Storage credentials updated");
                } catch (Exception e) {
                  error(e);
                }
              })
          .show();
    } catch (Exception e) {
      error(e);
    }
  }

  private void pick(int request) {
    Intent intent =
        new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)
            .addFlags(
                Intent.FLAG_GRANT_READ_URI_PERMISSION
                    | Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                    | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION
                    | Intent.FLAG_GRANT_PREFIX_URI_PERMISSION);
    startActivityForResult(intent, request);
  }

  @Override
  protected void onActivityResult(int request, int result, Intent data) {
    super.onActivityResult(request, result, data);
    if (result != RESULT_OK || data == null || data.getData() == null) return;
    Uri uri = data.getData();
    try {
      if ((data.getFlags() & Intent.FLAG_GRANT_READ_URI_PERMISSION) == 0)
        throw new Exception("The folder provider did not grant read access");
      if (request == 101) {
        if ((data.getFlags() & Intent.FLAG_GRANT_WRITE_URI_PERMISSION) == 0)
          throw new Exception("Choose a writable restore destination");
        getContentResolver()
            .takePersistableUriPermission(
                uri,
                Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
      } else if (request == 100)
        getContentResolver()
            .takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION);
      if (request == 100) {
        if (!idle()) return;
        JSONArray list = Settings.sources(this);
        for (int i = 0; i < list.length(); i++)
          if (uri.toString().equals(list.getJSONObject(i).getString("uri"))) {
            message("This folder is already selected");
            return;
          }
        LinearLayout f = form();
        EditText label = input(f, "Folder name in backup", "Folder-" + (list.length() + 1), false);
        new AlertDialog.Builder(this)
            .setTitle("Name backup folder")
            .setView(f)
            .setNegativeButton("Cancel", null)
            .setPositiveButton(
                "Add",
                (d, w) -> {
                  try {
                    String name = label.getText().toString().trim();
                    if (Paths.parts(name).length != 1
                        || name.equals("Installed-APKs")
                        || name.equals("Android-data")
                        || name.equals("Android-obb"))
                      throw new Exception("Choose a simple unique folder name");
                    for (int i = 0; i < list.length(); i++)
                      if (name.equals(list.getJSONObject(i).getString("label")))
                        throw new Exception("Folder name is already used");
                    list.put(new JSONObject().put("label", name).put("uri", uri.toString()));
                    Settings.prefs(this).edit().putString("sources", list.toString()).apply();
                    updateSummary();
                  } catch (Exception e) {
                    error(e);
                  }
                })
            .show();
      } else if (request == 101) {
        if (restoreStamp <= 0) throw new Exception("Choose a snapshot first");
        new AlertDialog.Builder(this)
            .setTitle("Restore backup?")
            .setMessage(
                "Restore the selected snapshot into a new PBS-X-restore folder here? Existing files"
                    + " will be preserved.")
            .setNegativeButton("Cancel", null)
            .setPositiveButton(
                "Restore",
                (d, w) ->
                    startOperation(
                        new Intent(this, BackupService.class)
                            .putExtra("destination", uri.toString())
                            .putExtra("source", restoreHost)
                            .putExtra("snapshot", restoreStamp)))
            .show();
      }
    } catch (Exception e) {
      error(e);
    }
  }

  private void removeSources() {
    if (!idle()) return;
    try {
      JSONArray list = Settings.sources(this);
      String[] names = new String[list.length()];
      boolean[] selected = new boolean[names.length];
      for (int i = 0; i < names.length; i++) names[i] = list.getJSONObject(i).getString("label");
      new AlertDialog.Builder(this)
          .setTitle("Remove selected folders")
          .setMultiChoiceItems(names, selected, (d, index, value) -> selected[index] = value)
          .setNegativeButton("Cancel", null)
          .setPositiveButton(
              "Remove",
              (d, w) -> {
                try {
                  JSONArray remaining = new JSONArray();
                  for (int i = 0; i < list.length(); i++)
                    if (!selected[i]) remaining.put(list.get(i));
                  Settings.prefs(this).edit().putString("sources", remaining.toString()).apply();
                  updateSummary();
                } catch (Exception e) {
                  error(e);
                }
              })
          .show();
    } catch (Exception e) {
      error(e);
    }
  }

  private void startOperation(Intent intent) {
    if (Build.VERSION.SDK_INT >= 33
        && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
            != PackageManager.PERMISSION_GRANTED)
      requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, 73);
    startForegroundService(intent);
  }

  private void snapshots() {
    if (!idle()) return;
    try {
      String raw = Vault.load(this);
      if (raw.isEmpty()) {
        message("Configure a connection first");
        return;
      }
      JSONObject cfg = new JSONObject(raw);
      LinearLayout f = form();
      EditText host = input(f, "Source host ID", cfg.getString("host_id"), false);
      text(
          f,
          "Restoring another host requires an API key with access to its backups. Change the"
              + " storage key first if needed.");
      new AlertDialog.Builder(this)
          .setTitle("Browse backups")
          .setView(f)
          .setNegativeButton("Cancel", null)
          .setPositiveButton(
              "Load",
              (d, w) -> {
                restoreHost = host.getText().toString().trim();
                background(
                    "Loading snapshots…",
                    () -> {
                      String response =
                          Operations.client(this, raw, null)
                              .listSnapshots(
                                  Settings.store(this), Settings.namespace(this), restoreHost);
                      JSONArray snapshots = new JSONObject(response).getJSONArray("data");
                      ArrayList<Long> times = new ArrayList<>();
                      for (int i = 0; i < snapshots.length(); i++)
                        times.add(snapshots.getJSONObject(i).getLong("backup-time"));
                      times.sort(java.util.Comparator.reverseOrder());
                      runOnUiThread(
                          () -> {
                            if (times.isEmpty()) {
                              message("No snapshots found");
                              return;
                            }
                            String[] labels = new String[times.size()];
                            for (int i = 0; i < labels.length; i++)
                              labels[i] =
                                  DateFormat.getDateTimeInstance()
                                      .format(new Date(times.get(i) * 1000));
                            new AlertDialog.Builder(this)
                                .setTitle("Choose snapshot")
                                .setItems(
                                    labels,
                                    (dialog, index) -> {
                                      restoreStamp = times.get(index);
                                      pick(101);
                                    })
                                .setNegativeButton("Cancel", null)
                                .show();
                          });
                      return "Loaded " + times.size() + " snapshots";
                    });
              })
          .show();
    } catch (Exception e) {
      error(e);
    }
  }

  private void settings() {
    if (!idle()) return;
    SharedPreferences p = Settings.prefs(this);
    LinearLayout f = form();
    EditText store = input(f, "Datastore", Settings.store(this), false),
        ns = input(f, "Namespace (optional)", Settings.namespace(this), false),
        hours =
            input(
                f,
                "Schedule interval in hours; 0 = manual",
                String.valueOf(p.getInt("hours", 0)),
                false);
    hours.setInputType(InputType.TYPE_CLASS_NUMBER);
    CheckBox wifi = check(f, "Wi-Fi only (manual and scheduled)", p.getBoolean("wifi", true)),
        charging =
            check(f, "Require charging for scheduled backups", p.getBoolean("charging", true)),
        shizuku = check(f, "Enable Shizuku mode", p.getBoolean("shizuku", false));
    text(
        f,
        "Shizuku must be running with permission granted. Shell access cannot read private app"
            + " data. Android/data and obb access varies by device. A failed source fails the"
            + " backup.");
    CheckBox apks = check(f, "Installed application APKs", p.getBoolean("apks", false)),
        data = check(f, "External app files: Android/data", p.getBoolean("data", false)),
        obb = check(f, "Application expansion files: Android/obb", p.getBoolean("obb", false));
    button(
        f,
        "Grant Shizuku permission",
        v -> {
          try {
            if (!Shizuku.pingBinder())
              throw new Exception("Start Shizuku using wireless debugging, then retry");
            if (Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED)
              message("Shizuku permission is already granted");
            else Shizuku.requestPermission(74);
          } catch (Exception e) {
            error(e);
          }
        });
    button(
        f,
        "Open Android app settings",
        v ->
            startActivity(
                new Intent(
                    android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                    Uri.parse("package:" + getPackageName()))));
    text(
        f,
        "Samsung: add PBS-X Backup to Battery → Background usage limits → Never sleeping apps if"
            + " using a schedule. Android may delay or interrupt scheduled work. Manual is the"
            + " default. All selected folders share this schedule.");
    ScrollView scroll = new ScrollView(this);
    scroll.addView(f);
    new AlertDialog.Builder(this)
        .setTitle("Backup settings")
        .setView(scroll)
        .setNegativeButton("Cancel", null)
        .setPositiveButton(
            "Save",
            (d, w) -> {
              try {
                int interval = Integer.parseInt(hours.getText().toString());
                if (interval < 0 || interval > 720)
                  throw new Exception("Schedule must be 0–720 hours");
                if (store.getText().toString().trim().isEmpty())
                  throw new Exception("Datastore is required");
                p.edit()
                    .putString("store", store.getText().toString().trim())
                    .putString("namespace", ns.getText().toString().trim())
                    .putInt("hours", interval)
                    .putBoolean("wifi", wifi.isChecked())
                    .putBoolean("charging", charging.isChecked())
                    .putBoolean("shizuku", shizuku.isChecked())
                    .putBoolean("apks", apks.isChecked())
                    .putBoolean("data", data.isChecked())
                    .putBoolean("obb", obb.isChecked())
                    .apply();
                ScheduleService.configure(this, interval, charging.isChecked());
                updateSummary();
              } catch (Exception e) {
                error(e);
              }
            })
        .show();
  }
}
