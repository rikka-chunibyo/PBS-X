package com.pbsx.android;

import android.app.Activity;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.Editable;
import android.text.TextWatcher;
import android.widget.*;
import com.pbsx.core.mobilecore.Client;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.text.DateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.json.JSONArray;
import org.json.JSONObject;

/** Permission-filtered storage catalogue, independent of this phone's local jobs. */
final class BackupCatalogue implements AutoCloseable {
  interface OpenGroup {
    void open(JSONObject group);
  }

  private final Activity activity;
  private final OpenGroup open;
  private final Runnable settings;
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private final Handler handler = new Handler(Looper.getMainLooper());
  private LinearLayout panel, groups;
  private JSONArray data, warnings = new JSONArray();
  private String identity = "", query = "", error = "";
  private int page;
  private volatile int generation;
  private boolean active, loading, closed;
  private long loaded;
  private volatile Client client;
  private Operations.Cancellation cancellation;
  private boolean chooseHistory;
  private final Runnable filterUpdate =
      () -> {
        page = 0;
        renderGroups();
      };

  BackupCatalogue(Activity a, OpenGroup open, Runnable settings, Bundle saved) {
    this.activity = a;
    this.open = open;
    this.settings = settings;
    if (saved != null) {
      query = saved.getString("catalogue_query", "");
      page = saved.getInt("catalogue_page", 0);
    }
  }

  void chooseHistory() {
    chooseHistory = true;
  }

  void save(Bundle out) {
    out.putString("catalogue_query", query);
    out.putInt("catalogue_page", page);
  }

  void setActive(boolean value) {
    active = value;
    if (!value) {
      cancel();
      panel = null;
      groups = null;
      handler.removeCallbacks(filterUpdate);
    }
  }

  private void cancel() {
    generation++;
    if (cancellation != null) cancellation.cancel();
    if (client != null) client.cancel();
    loading = false;
  }

  void render(LinearLayout target) {
    panel = target;
    try {
      String config = Vault.load(activity);
      String fingerprint =
          android.util.Base64.encodeToString(
              MessageDigest.getInstance("SHA-256").digest(config.getBytes(StandardCharsets.UTF_8)),
              android.util.Base64.NO_WRAP);
      if (!fingerprint.equals(identity)) {
        cancel();
        identity = fingerprint;
        data = null;
        warnings = new JSONArray();
        error = "";
        page = 0;
      }
      if (config.isEmpty()) {
        panel.removeAllViews();
        Ui.title(panel, chooseHistory ? "Choose backup history" : "Restore");
        Ui.hint(Ui.card(panel), "Connect to PBS-X to see the backups your storage key can access.");
        Ui.button(panel, "Open Settings", true, v -> settings.run());
        return;
      }
      draw();
      if (!loading
          && error.isEmpty()
          && (data == null || System.currentTimeMillis() - loaded > 30000)) load();
    } catch (Exception e) {
      error = e.getMessage() == null ? "Could not load the saved connection" : e.getMessage();
      draw();
    }
  }

  private void load() {
    if (!active || closed) return;
    cancel();
    int ticket = ++generation;
    loading = true;
    error = "";
    draw();
    final Operations.Cancellation token = new Operations.Cancellation();
    cancellation = token;
    executor.execute(
        () -> {
          try {
            token.check();
            Client c = Operations.client(activity, Vault.load(activity), token);
            client = c;
            token.check();
            JSONObject result = new JSONObject(c.listAccessibleBackups());
            activity.runOnUiThread(
                () -> {
                  if (ticket != generation || closed || !active || activity.isDestroyed()) return;
                  loading = false;
                  data = result.optJSONArray("groups");
                  if (data == null) data = new JSONArray();
                  warnings = result.optJSONArray("warnings");
                  if (warnings == null) warnings = new JSONArray();
                  loaded = System.currentTimeMillis();
                  draw();
                });
          } catch (Exception e) {
            activity.runOnUiThread(
                () -> {
                  if (ticket != generation || closed || !active || activity.isDestroyed()) return;
                  loading = false;
                  data = null;
                  error = e.getMessage() == null ? "Could not load backups" : e.getMessage();
                  draw();
                });
          }
        });
  }

  private void draw() {
    if (panel == null || closed) return;
    panel.removeAllViews();
    Ui.title(panel, chooseHistory ? "Choose backup history" : "Restore");
    Ui.hint(
        panel,
        chooseHistory
            ? "Choose a host backup history. Permission to list a group does not necessarily allow"
                  + " appending snapshots."
            : "All backup groups visible to your storage key, across datastores and namespaces.");
    Button refresh =
        Ui.button(panel, loading ? "Loading backups…" : "Refresh backups", false, v -> load());
    refresh.setEnabled(!loading);
    if (loading) {
      ProgressBar bar = new ProgressBar(activity, null, android.R.attr.progressBarStyleHorizontal);
      bar.setIndeterminate(true);
      panel.addView(bar, new LinearLayout.LayoutParams(-1, Ui.dp(activity, 8)));
    }
    EditText search = Ui.input(panel, "Search backups", query, false);
    search.setFilters(
        new android.text.InputFilter[] {new android.text.InputFilter.LengthFilter(256)});
    search.setContentDescription("Search backups");
    search.addTextChangedListener(
        new TextWatcher() {
          public void beforeTextChanged(CharSequence s, int start, int count, int after) {}

          public void onTextChanged(CharSequence s, int start, int before, int count) {
            query = s.toString();
            handler.removeCallbacks(filterUpdate);
            handler.postDelayed(filterUpdate, 150);
          }

          public void afterTextChanged(Editable value) {}
        });
    if (!error.isEmpty())
      Ui.hint(
          Ui.card(panel),
          "Could not load backups. "
              + error
              + "\nCheck the connection and storage key, then refresh.");
    if (warnings.length() > 0) {
      LinearLayout notice = Ui.card(panel);
      Ui.text(notice, "Some locations could not be listed", 16, Ui.BLUE);
      for (int i = 0; i < warnings.length(); i++) Ui.hint(notice, warnings.optString(i));
      Ui.hint(notice, "Refresh to retry. This list may be incomplete.");
    }
    groups = Ui.column(activity);
    panel.addView(groups);
    renderGroups();
  }

  private void renderGroups() {
    if (groups == null || closed) return;
    groups.removeAllViews();
    if (data == null) return;
    ArrayList<JSONObject> matches = new ArrayList<>();
    String term = query.toLowerCase(Locale.ROOT).trim();
    for (int i = 0; i < data.length(); i++) {
      JSONObject group = data.optJSONObject(i);
      if (group == null) continue;
      if (chooseHistory && !"host".equals(group.optString("backup_type"))) continue;
      String haystack =
          group.optString("backup_type")
              + "/"
              + group.optString("backup_id")
              + " "
              + group.optString("store")
              + " "
              + group.optString("namespace")
              + " "
              + group.optString("comment");
      if (term.isEmpty() || haystack.toLowerCase(Locale.ROOT).contains(term)) matches.add(group);
    }
    if (matches.isEmpty()) {
      if (!loading)
        Ui.hint(
            Ui.card(groups),
            data.length() == 0
                ? "No backups are visible to this storage key yet."
                : "No backups match your search.");
      return;
    }
    page = Math.max(0, Math.min(page, (matches.size() - 1) / 100));
    Ui.hint(
        groups,
        matches.size() + " backup groups" + (term.isEmpty() ? "" : " matching your search"));
    for (int i = page * 100; i < Math.min((page + 1) * 100, matches.size()); i++) {
      JSONObject group = matches.get(i);
      LinearLayout card = Ui.card(groups);
      Ui.text(
          card, group.optString("backup_type") + "/" + group.optString("backup_id"), 21, Ui.TEXT);
      String ns = group.optString("namespace");
      Ui.hint(card, group.optString("store") + " / " + (ns.isEmpty() ? "root" : ns));
      long stamp = group.optLong("last_backup");
      Ui.hint(
          card,
          group.optLong("backup_count")
              + " snapshots"
              + (stamp > 0
                  ? " · Latest "
                      + DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT)
                          .format(new Date(stamp * 1000))
                  : ""));
      String comment = group.optString("comment");
      if (!comment.isEmpty()) Ui.hint(card, comment);
      Ui.button(card, chooseHistory ? "Use this backup" : "Snapshots", true, v -> open.open(group));
    }
    if (matches.size() > 100) {
      LinearLayout pager = Ui.row(activity);
      Button previous =
          Ui.button(
              pager,
              "Previous",
              false,
              v -> {
                page--;
                renderGroups();
              });
      previous.setEnabled(page > 0);
      Button next =
          Ui.button(
              pager,
              "Next",
              false,
              v -> {
                page++;
                renderGroups();
              });
      next.setEnabled((page + 1) * 100 < matches.size());
      groups.addView(pager);
      Ui.hint(
          groups,
          (page * 100 + 1)
              + "–"
              + Math.min((page + 1) * 100, matches.size())
              + " of "
              + matches.size());
    }
  }

  @Override
  public void close() {
    closed = true;
    cancel();
    handler.removeCallbacks(filterUpdate);
    executor.shutdownNow();
    panel = null;
    groups = null;
  }
}
