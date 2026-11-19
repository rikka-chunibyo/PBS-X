package com.pbsx.android;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.text.TextUtils;
import android.view.ViewGroup;
import android.widget.*;
import com.pbsx.core.mobilecore.Client;
import java.text.DateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.TreeSet;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.json.JSONArray;
import org.json.JSONObject;

/** Snapshot history → paged file browser → selection → restore review. */
public final class RestoreActivity extends Activity {
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private final TreeSet<String> selected = new TreeSet<>();
  private LinearLayout page, footer;
  private String store = "backup", namespace = "", source = "", relative = ".", mode = "fail";
  private String connectionID = "";
  private String sourceType = "host";
  private long snapshot, offset, total;
  private int stage, generation, snapshotOffset;
  private boolean loading;
  private JSONArray snapshots = new JSONArray(), entries = new JSONArray();
  private volatile Client client;
  private Operations.Cancellation request;

  @Override
  public void onCreate(Bundle saved) {
    super.onCreate(saved);
    try {
      connectionID =
          saved == null
              ? getIntent().getStringExtra("connection_id")
              : saved.getString("connection_id", "");
      if (connectionID == null) connectionID = "";
      String config = KeyProfiles.config(this, connectionID, "", false);
      if (config.isEmpty()) throw new Exception("Connect to PBS-X in Settings first");
      JSONObject cfg = new JSONObject(config);
      source = cfg.getString("host_id");
      if (getIntent().hasExtra("source")) source = getIntent().getStringExtra("source");
      if (getIntent().hasExtra("source_type"))
        sourceType = getIntent().getStringExtra("source_type");
      store =
          getIntent().getStringExtra("store") == null
              ? Settings.store(this)
              : getIntent().getStringExtra("store");
      namespace =
          getIntent().getStringExtra("namespace") == null
              ? Settings.namespace(this)
              : getIntent().getStringExtra("namespace");
      LinearLayout root = Ui.screen(this, "Restore files");
      ((ViewGroup) root.getChildAt(0)).getChildAt(0).setOnClickListener(v -> navigateBack());
      if (Build.VERSION.SDK_INT >= 33)
        getOnBackInvokedDispatcher()
            .registerOnBackInvokedCallback(
                android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT, this::navigateBack);
      page = Ui.scroll(root);
      footer = Ui.column(this);
      footer.setPadding(Ui.dp(this, 16), Ui.dp(this, 5), Ui.dp(this, 16), Ui.dp(this, 8));
      footer.setBackgroundColor(Ui.SURFACE);
      root.addView(footer);
      if (saved != null) {
        store = saved.getString("store", store);
        namespace = saved.getString("namespace", namespace);
        source = saved.getString("source", source);
        sourceType = saved.getString("source_type", sourceType);
        relative = saved.getString("relative", ".");
        snapshot = saved.getLong("snapshot", 0);
        mode = saved.getString("mode", "fail");
        JSONArray paths = new JSONArray(saved.getString("selected", "[]"));
        for (int i = 0; i < paths.length(); i++) selected.add(paths.getString(i));
        if (snapshot > 0) {
          stage = 2;
          loadDirectory(relative, saved.getLong("offset", 0));
          return;
        }
      }
      loadSnapshots();
    } catch (Exception e) {
      message(e.getMessage());
    }
  }

  private void showError(String error) {
    stage = 0;
    loading = false;
    page.removeAllViews();
    footer.removeAllViews();
    Ui.title(page, "Could not load this backup");
    Ui.hint(
        page,
        sourceType
            + "/"
            + source
            + " · "
            + store
            + " / "
            + (namespace.isEmpty() ? "root" : namespace));
    Ui.hint(
        Ui.card(page), error == null ? "Check the connection and storage key, then retry." : error);
    Ui.button(page, "Retry snapshots", true, v -> loadSnapshots());
    Ui.button(footer, "Back", false, v -> finish());
  }

  interface Fetch {
    String run(Client c, Operations.Cancellation token) throws Exception;
  }

  interface Result {
    void show(String value) throws Exception;
  }

  private void fetch(String label, Fetch action, Result result) {
    fetch(label, action, result, this::showError);
  }

  private void fetch(
      String label, Fetch action, Result result, java.util.function.Consumer<String> failure) {
    final int ticket = ++generation;
    if (request != null) request.cancel();
    if (client != null) client.cancel();
    final Operations.Cancellation token = new Operations.Cancellation();
    request = token;
    loading = true;
    page.removeAllViews();
    footer.removeAllViews();
    Ui.title(page, label);
    Ui.hint(page, "Reading snapshot metadata…");
    executor.execute(
        () -> {
          try {
            token.check();
            Client c =
                Operations.client(this, KeyProfiles.config(this, connectionID, "", false), token);
            client = c;
            token.check();
            String response = action.run(c, token);
            runOnUiThread(
                () -> {
                  if (ticket != generation || isDestroyed() || isFinishing()) return;
                  loading = false;
                  try {
                    result.show(response);
                  } catch (Exception e) {
                    failure.accept(e.getMessage());
                  }
                });
          } catch (Exception e) {
            runOnUiThread(
                () -> {
                  if (ticket != generation || isDestroyed() || isFinishing()) return;
                  loading = false;
                  failure.accept(e.getMessage());
                });
          }
        });
  }

  private void loadSnapshots() {
    fetch(
        "Loading snapshots",
        (c, token) -> c.listGroupSnapshots(store, namespace, sourceType, source),
        value -> {
          JSONArray all = new JSONObject(value).getJSONArray("data");
          ArrayList<JSONObject> list = new ArrayList<>();
          for (int i = 0; i < all.length(); i++) list.add(all.getJSONObject(i));
          list.sort((a, b) -> Long.compare(b.optLong("backup-time"), a.optLong("backup-time")));
          snapshots = new JSONArray();
          for (JSONObject item : list) snapshots.put(item);
          snapshotOffset = 0;
          showSnapshots();
        });
  }

  private void showSnapshots() {
    stage = 1;
    page.removeAllViews();
    footer.removeAllViews();
    Ui.title(page, "Snapshots");
    Ui.hint(
        page,
        sourceType
            + "/"
            + source
            + " · "
            + store
            + " / "
            + (namespace.isEmpty() ? "root" : namespace));
    if (snapshots.length() == 0)
      Ui.hint(Ui.card(page), "No completed snapshots are available for this source.");
    for (int i = snapshotOffset; i < Math.min(snapshotOffset + 100, snapshots.length()); i++) {
      final JSONObject item = snapshots.optJSONObject(i);
      if (item == null) continue;
      long time = item.optLong("backup-time");
      LinearLayout card = Ui.card(page);
      Ui.text(card, date(time), 19, Ui.TEXT);
      String comment = item.optString("comment", "");
      if (!comment.isEmpty()) Ui.hint(card, comment);
      boolean browsable = canBrowse(item);
      if (!browsable)
        Ui.hint(
            card,
            "This snapshot has no supported root file archive. Use PVE/PBS tools to restore disk"
                + " images or other archive layouts.");
      Button browse =
          Ui.button(
              card,
              "Browse files",
              true,
              v -> {
                snapshot = time;
                relative = ".";
                selected.clear();
                offset = 0;
                stage = 2;
                loadDirectory(".", 0);
              });
      browse.setEnabled(browsable);
    }
    LinearLayout pager = Ui.row(this);
    if (snapshotOffset > 0)
      Ui.button(
          pager,
          "Newer",
          false,
          v -> {
            snapshotOffset = Math.max(0, snapshotOffset - 100);
            showSnapshots();
          });
    if (snapshotOffset + 100 < snapshots.length())
      Ui.button(
          pager,
          "Older",
          false,
          v -> {
            snapshotOffset += 100;
            showSnapshots();
          });
    page.addView(pager);
    LinearLayout controls = Ui.row(this);
    Ui.button(controls, "Refresh snapshots", false, v -> loadSnapshots());
    Ui.button(
        controls,
        getIntent().getBooleanExtra("from_catalogue", false) ? "Back to backups" : "Back to jobs",
        false,
        v -> finish());
    footer.addView(controls);
  }

  private boolean canBrowse(JSONObject item) {
    JSONArray files = item.optJSONArray("files");
    if (files == null) return sourceType.equals("host");
    boolean metadata = false, payload = false;
    for (int i = 0; i < files.length(); i++) {
      Object file = files.opt(i);
      String name =
          file instanceof JSONObject
              ? ((JSONObject) file).optString("filename")
              : String.valueOf(file);
      if (name.equals("root.pxar.didx")) return true;
      if (name.equals("root.mpxar.didx")) metadata = true;
      if (name.equals("root.ppxar.didx")) payload = true;
    }
    return metadata && payload;
  }

  private void loadDirectory(String path, long start) {
    relative = path;
    offset = start;
    stage = 2;
    fetch(
        "Opening folder",
        (c, token) ->
            c.listGroupArchive(store, namespace, sourceType, source, snapshot, path, start, 200),
        value -> {
          JSONObject result = new JSONObject(value);
          entries = result.getJSONArray("entries");
          total = result.getLong("total");
          renderDirectory();
        });
  }

  private void renderDirectory() {
    stage = 2;
    page.removeAllViews();
    footer.removeAllViews();
    Ui.text(page, date(snapshot), 14, Ui.MUTED);
    Ui.title(
        page,
        relative.equals(".")
            ? "Snapshot contents"
            : relative.substring(relative.lastIndexOf('/') + 1));
    Ui.hint(page, "/" + (relative.equals(".") ? "" : relative));
    LinearLayout top = Ui.row(this);
    if (!relative.equals(".")) Ui.button(top, "↑ Parent folder", false, v -> up());
    Ui.button(
        top,
        total == 0 ? "Select empty folder" : "Select all",
        false,
        v -> {
          try {
            if (total == 0) {
              selectPath(relative);
              renderDirectory();
            } else selectAll();
          } catch (Exception e) {
            message(e.getMessage());
          }
        });
    page.addView(top);
    if (total == 0) Ui.hint(page, "This folder is empty. You can still select and restore it.");
    for (int i = 0; i < entries.length(); i++) {
      JSONObject e = entries.optJSONObject(i);
      if (e == null) continue;
      String path = e.optString("path"), name = e.optString("name");
      boolean dir = e.optBoolean("directory"), supported = e.optBoolean("supported");
      LinearLayout row = Ui.card(page), line = Ui.row(this);
      CheckBox check = new CheckBox(this);
      boolean inherited = parentSelected(path);
      check.setChecked(selected.contains(path) || inherited);
      check.setEnabled(supported && !inherited);
      check.setContentDescription("Select " + name);
      line.addView(check, new LinearLayout.LayoutParams(Ui.dp(this, 46), Ui.dp(this, 52)));
      LinearLayout info = Ui.column(this);
      TextView label = Ui.text(info, name, 16, dir ? Ui.BLUE : Ui.TEXT);
      label.setMaxLines(2);
      label.setEllipsize(TextUtils.TruncateAt.END);
      Ui.hint(
          info,
          !supported
              ? "Unsupported Android file type"
              : dir
                  ? "Folder"
                  : android.text.format.Formatter.formatFileSize(this, e.optLong("size")));
      line.addView(info, new LinearLayout.LayoutParams(0, -2, 1));
      if (dir) {
        Button open = new Button(this);
        open.setText("Open");
        open.setAllCaps(false);
        open.setOnClickListener(v -> loadDirectory(path, 0));
        line.addView(open);
        info.setOnClickListener(v -> loadDirectory(path, 0));
      } else
        info.setOnClickListener(
            v -> {
              if (check.isEnabled()) check.performClick();
            });
      row.addView(line);
      check.setOnCheckedChangeListener(
          (button, checked) -> {
            if (checked) {
              try {
                selectPath(path);
              } catch (Exception failure) {
                button.setChecked(false);
                message(failure.getMessage());
              }
            } else selected.remove(path);
            if (dir) {
              android.widget.ScrollView scroll = (android.widget.ScrollView) page.getParent();
              int y = scroll.getScrollY();
              renderDirectory();
              scroll.post(() -> scroll.scrollTo(0, y));
            } else renderSelection();
          });
    }
    if (total > 200) {
      LinearLayout pager = Ui.row(this);
      if (offset > 0)
        Ui.button(
            pager, "Previous", false, v -> loadDirectory(relative, Math.max(0, offset - 200)));
      if (offset + 200 < total)
        Ui.button(pager, "Next", false, v -> loadDirectory(relative, offset + 200));
      page.addView(pager);
      Ui.hint(page, (offset + 1) + "–" + Math.min(offset + 200, total) + " of " + total);
    }
    renderSelection();
  }

  private boolean parentSelected(String path) {
    for (String parent : selected)
      if (parent.equals(".") || path.startsWith(parent + "/")) return true;
    return false;
  }

  private void selectAll() throws Exception {
    if (offset == 0 && entries.length() == total) {
      selectEntries(entries);
      renderDirectory();
      return;
    }
    String folder = relative;
    long expected = total;
    fetch(
        "Selecting files and folders",
        (c, token) -> {
          JSONArray all = new JSONArray();
          for (long start = 0; start < expected; ) {
            token.check();
            JSONObject result =
                new JSONObject(
                    c.listGroupArchive(
                        store, namespace, sourceType, source, snapshot, folder, start, 200));
            JSONArray batch = result.getJSONArray("entries");
            if (result.getLong("total") != expected || batch.length() == 0)
              throw new Exception("Folder listing changed; reopen it and retry");
            for (int i = 0; i < batch.length(); i++) {
              JSONObject item = batch.getJSONObject(i);
              if (item.getBoolean("supported")) all.put(item);
              if (all.length() > 1000)
                throw new Exception(
                    "Select up to 1,000 paths at a time, or select this folder from its parent.");
            }
            start += batch.length();
          }
          return all.toString();
        },
        value -> {
          selectEntries(new JSONArray(value));
          renderDirectory();
        },
        error -> {
          renderDirectory();
          message(error);
        });
  }

  private void selectEntries(JSONArray items) throws Exception {
    TreeSet<String> before = new TreeSet<>(selected);
    try {
      // Expand a formerly selected whole folder into its individual children.
      selected.remove(relative);
      for (int i = 0; i < items.length(); i++) {
        JSONObject item = items.getJSONObject(i);
        if (item.getBoolean("supported")) selectPath(item.getString("path"));
      }
    } catch (Exception e) {
      selected.clear();
      selected.addAll(before);
      throw e;
    }
  }

  private void selectPath(String path) throws Exception {
    if (parentSelected(path)) return;
    TreeSet<String> before = new TreeSet<>(selected);
    if (path.equals(".")) selected.clear();
    else selected.removeIf(p -> p.startsWith(path + "/"));
    selected.add(path);
    if (selected.size() > 1000 || selectionJSON().length() > 256 * 1024) {
      selected.clear();
      selected.addAll(before);
      throw new Exception("Select a parent folder instead of more than 1,000 individual paths");
    }
  }

  private String selectionJSON() {
    return new JSONArray(selected).toString();
  }

  private void renderSelection() {
    footer.removeAllViews();
    LinearLayout row = Ui.row(this);
    Ui.button(row, "Selection · " + selected.size(), false, v -> selectionDialog());
    Button restore = Ui.button(row, "Restore selected", true, v -> options());
    restore.setEnabled(!selected.isEmpty());
    footer.addView(row);
    Ui.hint(footer, "Selected paths keep their folder hierarchy at the destination.");
  }

  private void selectionDialog() {
    if (selected.isEmpty()) {
      message("Choose files or folders using their checkboxes.");
      return;
    }
    String[] paths = selected.toArray(new String[0]);
    boolean[] remove = new boolean[paths.length];
    AlertDialog dialog =
        new AlertDialog.Builder(this)
            .setTitle("Remove from selection")
            .setMultiChoiceItems(paths, remove, (d, index, value) -> remove[index] = value)
            .setNegativeButton("Invert selection", null)
            .setPositiveButton(
                "Remove selected",
                (d, w) -> {
                  for (int i = 0; i < paths.length; i++) if (remove[i]) selected.remove(paths[i]);
                  renderDirectory();
                })
            .setNeutralButton(
                "Clear all",
                (d, w) -> {
                  selected.clear();
                  renderDirectory();
                })
            .create();
    dialog.setOnShowListener(
        d ->
            dialog
                .getButton(AlertDialog.BUTTON_NEGATIVE)
                .setOnClickListener(
                    v -> {
                      for (int i = 0; i < remove.length; i++) {
                        remove[i] = !remove[i];
                        dialog.getListView().setItemChecked(i, remove[i]);
                      }
                    }));
    dialog.show();
  }

  private void options() {
    if (Operations.running()) {
      message("Wait for the current transfer or cancel it first");
      return;
    }
    LinearLayout form = Ui.form(this);
    Ui.hint(form, selected.size() + " selected paths · " + source + "\n" + date(snapshot));
    String[] codes = {"fail", "skip", "replace", "rename"},
        labels =
            {
              "New folder (default)",
              "Skip existing files",
              "Replace different files",
              "Rename conflicting files"
            },
        details =
            {
              "Create a separate PBS-X-restore folder. Existing destination files are untouched.",
              "Add missing files. Leave existing files untouched and reuse existing folders.",
              "Add missing files and replace files only when their contents differ. Reuse folders"
                  + " and keep unrelated files. File/folder type conflicts stop the restore.",
              "Add missing files and keep both copies on conflicts using a numbered suffix. Reuse"
                  + " existing folders."
            };
    RadioGroup modes = new RadioGroup(this);
    int[] ids = new int[labels.length];
    for (int i = 0; i < labels.length; i++) {
      RadioButton b = new RadioButton(this);
      b.setText(labels[i]);
      ids[i] = android.view.View.generateViewId();
      b.setId(ids[i]);
      modes.addView(b);
    }
    form.addView(modes);
    TextView explanation = Ui.hint(form, "");
    int current = 0;
    for (int i = 0; i < codes.length; i++) if (codes[i].equals(mode)) current = i;
    modes.check(ids[current]);
    mode = codes[current];
    explanation.setText(details[current]);
    modes.setOnCheckedChangeListener(
        (g, id) -> {
          for (int index = 0; index < ids.length; index++)
            if (ids[index] == id) {
              mode = codes[index];
              explanation.setText(details[index]);
              break;
            }
        });
    new AlertDialog.Builder(this)
        .setTitle("Restore options")
        .setView(Ui.wrapped(form))
        .setNegativeButton("Cancel", null)
        .setPositiveButton("Choose destination", (d, w) -> chooseDestination())
        .show();
  }

  private void chooseDestination() {
    new AlertDialog.Builder(this)
        .setTitle("Restore destination")
        .setItems(
            new String[] {"Android folder picker", "Folder via Shizuku"},
            (dialog, index) -> {
              if (index == 0) {
                startActivityForResult(
                    new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)
                        .addFlags(
                            Intent.FLAG_GRANT_READ_URI_PERMISSION
                                | Intent.FLAG_GRANT_WRITE_URI_PERMISSION
                                | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION
                                | Intent.FLAG_GRANT_PREFIX_URI_PERMISSION),
                    101);
              } else {
                new ShizukuFolderPicker(
                        this,
                        executor,
                        path ->
                            review(
                                Uri.fromParts("pbsx-shizuku", path, null),
                                (path.equals(".") ? "Internal shared storage" : "/" + path)
                                    + " via Shizuku"),
                        true)
                    .show();
              }
            })
        .setNegativeButton("Cancel", null)
        .show();
  }

  @Override
  protected void onActivityResult(int code, int result, Intent intent) {
    super.onActivityResult(code, result, intent);
    if (code != 101 || result != RESULT_OK || intent == null || intent.getData() == null) return;
    try {
      Uri uri = intent.getData();
      if ((intent.getFlags() & Intent.FLAG_GRANT_READ_URI_PERMISSION) == 0
          || (intent.getFlags() & Intent.FLAG_GRANT_WRITE_URI_PERMISSION) == 0)
        throw new Exception("Choose a folder with read and write access");
      getContentResolver()
          .takePersistableUriPermission(
              uri, Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
      executor.execute(
          () -> {
            try {
              String name = new DocumentTree(getContentResolver(), uri).stat(".").getString("name");
              runOnUiThread(() -> review(uri, name));
            } catch (Exception e) {
              runOnUiThread(() -> message(e.getMessage()));
            }
          });
    } catch (Exception e) {
      message(e.getMessage());
    }
  }

  private void review(Uri uri, String name) {
    if (isDestroyed() || isFinishing()) return;
    StringBuilder paths = new StringBuilder();
    int count = 0;
    for (String p : selected) {
      if (count++ == 5) {
        paths.append("\n… and more");
        break;
      }
      paths.append("\n• ").append(p.equals(".") ? "Entire snapshot" : p);
    }
    String label =
        mode.equals("fail")
            ? "New folder"
            : mode.equals("skip")
                ? "Skip existing files"
                : mode.equals("replace") ? "Replace different files" : "Rename conflicts";
    new AlertDialog.Builder(this)
        .setTitle("Restore to " + name + "?")
        .setMessage(
            "Mode: "
                + label
                + "\n"
                + (mode.equals("replace")
                    ? "Different matching files will be overwritten; unrelated files stay in"
                        + " place.\n"
                    : "")
                + paths)
        .setNegativeButton("Cancel", null)
        .setPositiveButton(
            "Start restore",
            (d, w) -> {
              try {
                if (Operations.running())
                  throw new Exception("Another transfer is already running");
                BackupJob target = new BackupJob();
                target.connectionID = connectionID;
                target.name = "Restore from " + sourceType + "/" + source;
                target.backupID = source;
                target.store = store;
                target.namespace = namespace;
                target.wifi = Settings.prefs(this).getBoolean("wifi", true);
                if (Build.VERSION.SDK_INT >= 33
                    && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                        != PackageManager.PERMISSION_GRANTED)
                  requestPermissions(new String[] {Manifest.permission.POST_NOTIFICATIONS}, 73);
                startForegroundService(
                    new Intent(this, BackupService.class)
                        .putExtra("job_config", target.json().toString())
                        .putExtra("destination", uri.toString())
                        .putExtra("source", source)
                        .putExtra("source_type", sourceType)
                        .putExtra("snapshot", snapshot)
                        .putExtra("selection", selectionJSON())
                        .putExtra("mode", mode));
                startActivity(
                    new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP));
                finish();
              } catch (Exception e) {
                message(e.getMessage());
              }
            })
        .show();
  }

  private void up() {
    int slash = relative.lastIndexOf('/');
    loadDirectory(slash < 0 ? "." : relative.substring(0, slash), 0);
  }

  private void navigateBack() {
    generation++;
    if (request != null) request.cancel();
    if (client != null) client.cancel();
    loading = false;
    if (stage == 2 && !relative.equals(".")) {
      up();
      return;
    }
    if (stage == 2) {
      if (snapshots.length() > 0) showSnapshots();
      else loadSnapshots();
      return;
    }
    if (stage == 1) {
      finish();
      return;
    }
    finish();
  }

  // API 33+ uses the platform callback registered above. This override is the
  // compatibility fallback for the application's Android 8–12 support.
  @android.annotation.SuppressLint("GestureBackNavigation")
  @Override
  public void onBackPressed() {
    navigateBack();
  }

  @Override
  protected void onDestroy() {
    generation++;
    if (request != null) request.cancel();
    if (client != null) client.cancel();
    executor.shutdownNow();
    super.onDestroy();
  }

  @Override
  protected void onSaveInstanceState(Bundle out) {
    out.putString("connection_id", connectionID);
    super.onSaveInstanceState(out);
    out.putString("store", store);
    out.putString("namespace", namespace);
    out.putString("source", source);
    out.putString("source_type", sourceType);
    out.putString("relative", relative);
    out.putLong("snapshot", stage == 2 ? snapshot : 0);
    out.putLong("offset", offset);
    out.putString("mode", mode);
    out.putString("selected", selectionJSON());
  }

  private void message(String text) {
    if (!isDestroyed() && !isFinishing())
      new AlertDialog.Builder(this)
          .setMessage(text == null ? "Restore request failed" : text)
          .setPositiveButton("OK", null)
          .show();
  }

  private static String date(long seconds) {
    return DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT)
        .format(new Date(seconds * 1000));
  }
}
