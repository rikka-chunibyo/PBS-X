package com.pbsx.android;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.widget.*;
import java.text.DateFormat;
import java.util.Date;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.json.JSONArray;
import org.json.JSONObject;
import rikka.shizuku.Shizuku;

/** Jobs, restore, activity and connection settings have separate navigation. */
public final class MainActivity extends Activity {
  private final Handler handler = new Handler(Looper.getMainLooper());
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private LinearLayout page, nav, root;
  private TextView connectionLabel, activeLabel;
  private int tab;
  private long fingerprint;
  private boolean busy;
  private ConnectionDialogs connections;
  private final Runnable refresh =
      new Runnable() {
        public void run() {
          try {
            if (activeLabel != null)
              activeLabel.setText(
                  Operations.running()
                      ? Operations.activeName()
                          + "\n"
                          + Settings.prefs(MainActivity.this).getString("status", "Working…")
                      : "Ready · v" + BuildConfig.VERSION_NAME);
            long next = fingerprint();
            if (next != fingerprint) {
              fingerprint = next;
              render();
            }
          } catch (Exception e) {
            Settings.status(MainActivity.this, e.getMessage());
          }
          handler.postDelayed(this, 1500);
        }
      };
  private final Shizuku.OnRequestPermissionResultListener permission =
      (code, result) ->
          runOnUiThread(
              () -> {
                message(
                    result == PackageManager.PERMISSION_GRANTED
                        ? "Shizuku permission granted. Choose its sources in a job."
                        : "Shizuku permission denied. Folder backups still work.");
                render();
              });

  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    if (state != null) tab = state.getInt("tab", 0);
    Shizuku.addRequestPermissionResultListener(permission);
    connections = new ConnectionDialogs(this, this::background, this::render);
    root = Ui.column(this);
    root.setBackgroundColor(Ui.BG);
    Ui.systemBars(this);
    LinearLayout header = Ui.column(this);
    header.setBackgroundColor(Ui.NAVY);
    header.setPadding(Ui.dp(this, 22), Ui.dp(this, 18), Ui.dp(this, 22), Ui.dp(this, 16));
    TextView title = Ui.text(header, "PBS-X Backup", 26, Color.WHITE);
    title.setTypeface(null, Typeface.BOLD);
    connectionLabel = Ui.text(header, "", 13, Color.rgb(189, 204, 237));
    activeLabel = Ui.text(header, "", 13, Color.WHITE);
    root.addView(header);
    ScrollView scroll = new ScrollView(this);
    scroll.setFillViewport(true);
    page = Ui.column(this);
    page.setPadding(Ui.dp(this, 18), Ui.dp(this, 15), Ui.dp(this, 18), Ui.dp(this, 18));
    scroll.addView(page);
    root.addView(scroll, new LinearLayout.LayoutParams(-1, 0, 1));
    nav = Ui.row(this);
    nav.setBackgroundColor(Color.WHITE);
    nav.setPadding(Ui.dp(this, 4), Ui.dp(this, 4), Ui.dp(this, 4), Ui.dp(this, 4));
    root.addView(nav);
    if (Build.VERSION.SDK_INT >= 30)
      root.setOnApplyWindowInsetsListener(
          (v, insets) -> {
            android.graphics.Insets bars =
                insets.getInsets(android.view.WindowInsets.Type.systemBars());
            header.setPadding(
                Ui.dp(this, 22), Ui.dp(this, 18) + bars.top, Ui.dp(this, 22), Ui.dp(this, 16));
            nav.setPadding(
                Ui.dp(this, 4), Ui.dp(this, 4), Ui.dp(this, 4), Ui.dp(this, 4) + bars.bottom);
            v.setPadding(bars.left, 0, bars.right, 0);
            return insets;
          });
    setContentView(root);
    try {
      JobStore.all(this);
      JobStore.recover(this);
      ScheduleService.configureAll(this);
    } catch (Exception e) {
      message(e.getMessage());
    }
    render();
  }

  @Override
  protected void onResume() {
    super.onResume();
    try {
      JobStore.recover(this);
    } catch (Exception ignored) {
    }
    render();
    handler.post(refresh);
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
    out.putInt("tab", tab);
    super.onSaveInstanceState(out);
  }

  private long fingerprint() {
    android.content.SharedPreferences p = getSharedPreferences("jobs-v1", 0);
    return 31L * p.getString("jobs", "").hashCode() + p.getString("history", "").hashCode();
  }

  private void render() {
    if (page == null || isDestroyed() || isFinishing()) return;
    page.removeAllViews();
    nav.removeAllViews();
    try {
      String raw = Vault.load(this);
      JSONObject cfg = raw.isEmpty() ? null : new JSONObject(raw);
      connectionLabel.setText(
          cfg == null
              ? "Connect your backup server in Settings"
              : cfg.optString("host_id") + "  ·  " + cfg.optString("server"));
      activeLabel.setText(
          Operations.running()
              ? Operations.activeName()
                  + "\n"
                  + Settings.prefs(this).getString("status", "Working…")
              : "Ready · v" + BuildConfig.VERSION_NAME);
      String[] titles = {"Jobs", "Restore", "Activity", "Settings"};
      for (int i = 0; i < titles.length; i++) {
        final int next = i;
        Button b =
            Ui.button(
                nav,
                titles[i],
                i == tab,
                v -> {
                  tab = next;
                  render();
                });
        b.setTextSize(12);
      }
      if (Operations.running()) {
        LinearLayout running = Ui.card(page);
        Ui.text(running, "Transfer in progress", 17, Ui.BLUE);
        Ui.hint(running, Operations.activeName());
        Ui.button(running, "Cancel transfer", false, v -> Operations.cancel());
      }
      if (tab == 0) jobs(cfg);
      else if (tab == 1) restore(cfg);
      else if (tab == 2) activity();
      else settings(cfg);
      fingerprint = fingerprint();
    } catch (Exception e) {
      Ui.hint(page, e.getMessage() == null ? "Could not load app data" : e.getMessage());
    }
  }

  private void jobs(JSONObject cfg) throws Exception {
    LinearLayout heading = Ui.row(this);
    TextView title = new TextView(this);
    title.setText("Backup jobs");
    title.setTextSize(25);
    title.setTypeface(null, Typeface.BOLD);
    title.setTextColor(Ui.NAVY);
    heading.addView(title, new LinearLayout.LayoutParams(0, -2, 1));
    Button add = new Button(this);
    add.setText("+ New job");
    add.setAllCaps(false);
    add.setOnClickListener(v -> edit(null));
    heading.addView(add);
    page.addView(heading);
    Ui.hint(page, "Choose what to protect, where it goes and when each job runs.");
    if (cfg == null) {
      LinearLayout connect = Ui.card(page);
      Ui.text(connect, "Connect to PBS-X", 19, Ui.NAVY);
      Ui.hint(
          connect,
          "Use an enrollment code for an automatic device key, or connect with an existing API"
              + " token.");
      Ui.button(connect, "Enroll with code", true, v -> connections.enroll());
      Ui.button(connect, "Existing API key", false, v -> connections.manual());
    }
    List<BackupJob> jobs = JobStore.all(this);
    JSONArray history = JobStore.history(this);
    if (jobs.isEmpty()) {
      LinearLayout empty = Ui.card(page);
      Ui.title(empty, "Your first restore point");
      Ui.hint(
          empty,
          "Create a job for DCIM, documents or any folders you choose. Manual runs are the"
              + " default.");
      Ui.button(empty, "Create a backup job", true, v -> edit(null));
    }
    for (BackupJob job : jobs) {
      LinearLayout card = Ui.card(page);
      LinearLayout titleRow = Ui.row(this);
      TextView label = new TextView(this);
      label.setText(job.name);
      label.setTextSize(21);
      label.setTextColor(Ui.NAVY);
      label.setTypeface(null, Typeface.BOLD);
      titleRow.addView(label, new LinearLayout.LayoutParams(0, -2, 1));
      Button more = new Button(this);
      more.setText("⋮");
      more.setTextSize(23);
      more.setContentDescription("Job options for " + job.name);
      more.setBackgroundColor(Color.WHITE);
      titleRow.addView(more, new LinearLayout.LayoutParams(Ui.dp(this, 48), Ui.dp(this, 48)));
      more.setOnClickListener(
          v -> {
            PopupMenu menu = new PopupMenu(this, more);
            menu.getMenu()
                .add("Edit job")
                .setOnMenuItemClickListener(
                    item -> {
                      edit(job.id);
                      return true;
                    });
            menu.show();
          });
      card.addView(titleRow);
      Ui.hint(card, job.sourceCount() + " sources  ·  " + job.target());
      Ui.text(
          card, job.schedule() + (job.wifi ? "  ·  Wi-Fi only" : "  ·  any network"), 13, Ui.BLUE);
      JSONObject last = null;
      for (int i = 0; i < history.length(); i++)
        if (job.id.equals(history.getJSONObject(i).optString("job_id"))) {
          last = history.getJSONObject(i);
          break;
        }
      if (last == null) Ui.hint(card, "No runs yet");
      else Ui.hint(card, pretty(last.optString("state")) + " · " + date(last.optLong("started")));
      LinearLayout actions = Ui.row(this);
      Ui.button(actions, "Back up", true, v -> run(job));
      Ui.button(actions, "Snapshots", false, v -> browse(job));
      card.addView(actions);
    }
    Ui.hint(
        page,
        "Jobs sharing a destination use this host's PBS snapshot history. Each run contains the"
            + " folders selected in that job.");
  }

  private void restore(JSONObject cfg) throws Exception {
    Ui.title(page, "Restore files");
    Ui.hint(
        page,
        "Browse a snapshot, choose files or folders, then choose a destination and conflict mode.");
    LinearLayout intro = Ui.card(page);
    Ui.text(intro, "Only what you need", 21, Ui.NAVY);
    Ui.hint(
        intro,
        "Select individual files across folders, an entire directory, or the whole snapshot."
            + " Restore into a fresh folder, skip existing files, replace different files, or keep"
            + " both with Rename.");
    Ui.button(intro, "Browse backups", true, v -> browse(null));
    for (BackupJob job : JobStore.all(this)) {
      LinearLayout card = Ui.card(page);
      Ui.text(card, job.name, 18, Ui.NAVY);
      Ui.hint(card, job.target());
      Ui.button(card, "Browse this destination", false, v -> browse(job));
    }
    Ui.hint(
        page,
        "To restore another host's backups, enter its source host ID in the browser. Your storage"
            + " key must have access to that group.");
  }

  private void activity() throws Exception {
    Ui.title(page, "Activity");
    Ui.hint(page, "Recent backup and restore runs on this device.");
    JSONArray history = JobStore.history(this);
    if (history.length() == 0)
      Ui.hint(Ui.card(page), "Your completed and failed runs will appear here.");
    for (int i = 0; i < history.length(); i++) {
      JSONObject r = history.getJSONObject(i);
      LinearLayout card = Ui.card(page);
      String state = r.optString("state");
      Ui.text(
          card,
          pretty(state) + " · " + pretty(r.optString("kind")),
          14,
          "success".equals(state)
              ? Color.rgb(27, 121, 94)
              : "failed".equals(state) || "interrupted".equals(state)
                  ? Color.rgb(170, 65, 65)
                  : Ui.BLUE);
      Ui.text(card, r.optString("job_name"), 19, Ui.NAVY);
      Ui.hint(card, date(r.optLong("started")) + " · " + r.optString("target"));
      Ui.hint(card, r.optString("message"));
    }
  }

  private void settings(JSONObject cfg) {
    Ui.title(page, "Settings");
    LinearLayout account = Ui.card(page);
    Ui.text(account, "Server & credentials", 20, Ui.NAVY);
    Ui.hint(
        account,
        cfg == null
            ? "No server connected"
            : cfg.optString("server") + "\nBackup group: host/" + cfg.optString("host_id"));
    Ui.button(account, "Enroll with PBS-X code", true, v -> connections.enroll());
    Ui.button(account, "Use an existing API key", false, v -> connections.manual());
    Ui.button(account, "Change storage API key", false, v -> connections.storage());
    LinearLayout restore = Ui.card(page);
    Ui.text(restore, "Restore connection", 19, Ui.NAVY);
    CheckBox wifi =
        Ui.check(
            restore,
            "Wi-Fi only for browsing and restores",
            Settings.prefs(this).getBoolean("wifi", true));
    wifi.setOnCheckedChangeListener(
        (b, value) -> Settings.prefs(this).edit().putBoolean("wifi", value).apply());
    Ui.hint(restore, "Backup jobs each have their own Wi-Fi setting.");
    LinearLayout privileged = Ui.card(page);
    Ui.text(privileged, "Shizuku", 19, Ui.NAVY);
    String status = "Not running";
    try {
      if (Shizuku.pingBinder())
        status =
            Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED
                ? "Ready · permission granted"
                : "Running · permission needed";
    } catch (RuntimeException ignored) {
    }
    Ui.hint(privileged, status);
    Ui.button(
        privileged,
        "Grant Shizuku permission",
        false,
        v -> {
          try {
            if (!Shizuku.pingBinder())
              throw new Exception("Start Shizuku with wireless debugging, then retry");
            if (Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED)
              message("Permission is already granted. Select Shizuku sources in a job.");
            else Shizuku.requestPermission(74);
          } catch (Exception e) {
            message(e.getMessage());
          }
        });
    Ui.hint(
        privileged,
        "Jobs can opt into installed APKs and accessible Android/data or obb files. Private app"
            + " data is not included.");
    LinearLayout battery = Ui.card(page);
    Ui.text(battery, "Background backups", 19, Ui.NAVY);
    Ui.hint(
        battery,
        "On Samsung, add PBS-X Backup to Battery → Background usage limits → Never sleeping apps."
            + " Android can delay or interrupt schedules.");
    Ui.button(
        battery,
        "Android app settings",
        false,
        v ->
            startActivity(
                new Intent(
                    android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
                    Uri.parse("package:" + getPackageName()))));
    Ui.hint(page, "PBS-X Android " + BuildConfig.VERSION_NAME);
  }

  private void edit(String id) {
    Intent intent = new Intent(this, JobEditorActivity.class);
    if (id != null) intent.putExtra("job_id", id);
    startActivity(intent);
  }

  private void browse(BackupJob job) {
    if (busy) {
      message("Connection setup is still running");
      return;
    }
    Intent i = new Intent(this, RestoreActivity.class);
    if (job != null) i.putExtra("store", job.store).putExtra("namespace", job.namespace);
    startActivity(i);
  }

  private void run(BackupJob job) {
    try {
      if (busy) throw new Exception("Connection setup is still running");
      if (Operations.running())
        throw new Exception("Wait for the current transfer or cancel it first");
      if (Vault.load(this).isEmpty()) {
        tab = 3;
        render();
        throw new Exception("Connect to PBS-X first");
      }
      job.validate(true);
      if (Build.VERSION.SDK_INT >= 33
          && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
              != PackageManager.PERMISSION_GRANTED)
        requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, 73);
      startForegroundService(
          new Intent(this, BackupService.class).putExtra("job_config", job.json().toString()));
    } catch (Exception e) {
      message(e.getMessage());
    }
  }

  private void background(String label, ConnectionDialogs.Action action) {
    if (busy) {
      message("Another connection request is running");
      return;
    }
    busy = true;
    Settings.status(this, label);
    activeLabel.setText(label);
    executor.execute(
        () -> {
          String result;
          try {
            result = action.run();
          } catch (Exception e) {
            result = e.getMessage() == null ? "Connection failed" : e.getMessage();
          }
          Settings.status(this, result);
          final String shown = result;
          runOnUiThread(
              () -> {
                busy = false;
                if (!isFinishing() && !isDestroyed()) {
                  render();
                  message(shown);
                }
              });
        });
  }

  private void message(String value) {
    if (!isFinishing() && !isDestroyed())
      new AlertDialog.Builder(this)
          .setMessage(value == null ? "Operation failed" : value)
          .setPositiveButton("OK", null)
          .show();
  }

  private static String date(long t) {
    return DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(new Date(t));
  }

  private static String pretty(String value) {
    return value.isEmpty() ? value : Character.toUpperCase(value.charAt(0)) + value.substring(1);
  }
}
