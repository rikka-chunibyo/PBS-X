package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.text.InputType;
import android.widget.*;
import org.json.JSONArray;
import org.json.JSONObject;

public final class JobEditorActivity extends Activity {
  private BackupJob job;
  private EditText name, store, namespace, backupID, hours;
  private CheckBox wifi, charging, shizuku, apks, data, obb;
  private CheckBox allApps;
  private final CheckBox[] exportOptions = new CheckBox[DeviceExport.CATEGORIES.size()];
  private final java.util.concurrent.ExecutorService executor =
      java.util.concurrent.Executors.newSingleThreadExecutor();
  private TextView appSummary;
  private Button chooseApps;
  private boolean choosingApps;
  private LinearLayout folders;
  private Button connection, encryption;

  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    try {
      String id = getIntent().getStringExtra("job_id");
      job = id == null ? BackupJob.create(this) : JobStore.get(this, id);
      if (state != null) {
        job.connectionID = state.getString("connection_id", job.connectionID);
        job.encryptionKeyID = state.getString("encryption_key_id", job.encryptionKeyID);
        job.id = state.getString("job_id", job.id);
        job.sources = new JSONArray(state.getString("sources", "[]"));
        job.packages = new JSONArray(state.getString("packages", job.packages.toString()));
      }
      LinearLayout root = Ui.screen(this, id == null ? "New backup job" : "Edit backup job"),
          page = Ui.scroll(root);
      LinearLayout basic = Ui.card(page);
      Ui.title(basic, "Job details");
      name = Ui.input(basic, "Job name", job.name, false);
      Ui.hint(basic, "Display label only. Renaming this job does not change its backup history.");
      LinearLayout dest = Ui.card(page);
      Ui.text(dest, "Destination", 18, Ui.TEXT);
      connection = Ui.button(dest, "", false, v -> chooseCredential(false));
      encryption = Ui.button(dest, "", false, v -> chooseCredential(true));
      credentialLabels();
      store = Ui.input(dest, "Datastore", job.store, false);
      namespace = Ui.input(dest, "Namespace · leave blank for root", job.namespace, false);
      backupID = Ui.input(dest, "Backup ID", job.backupID(this), false);
      backupID.setContentDescription("Backup ID");
      Ui.button(
          dest,
          "Use existing backup",
          false,
          v ->
              startActivityForResult(
                  new Intent(this, SelectBackupActivity.class)
                      .putExtra("connection_id", job.connectionID),
                  101));
      Ui.hint(
          dest,
          "The datastore, namespace and Backup ID identify one snapshot history. Jobs using the"
              + " same destination share snapshots; each snapshot contains only that run's selected"
              + " sources, not a merge. Changing the destination leaves older snapshots in Restore."
              + " Your storage key needs permission for the namespace.");
      LinearLayout sourceCard = Ui.card(page);
      Ui.text(sourceCard, "Folders", 18, Ui.TEXT);
      folders = Ui.column(this);
      sourceCard.addView(folders);
      renderFolders();
      Ui.button(sourceCard, "+ Add folder", false, v -> pick());
      LinearLayout options = Ui.card(page);
      Ui.text(options, "Schedule & connection", 18, Ui.TEXT);
      hours =
          Ui.input(options, "Run every … hours · 0 means manual", String.valueOf(job.hours), false);
      hours.setInputType(InputType.TYPE_CLASS_NUMBER);
      wifi = Ui.check(options, "Wi-Fi only", job.wifi);
      charging = Ui.check(options, "Require charging for scheduled runs", job.charging);
      Ui.hint(
          options,
          "Android chooses the exact time. Each job has its own schedule; transfers run one at a"
              + " time.");
      LinearLayout extra = Ui.card(page);
      Ui.text(extra, "Optional Shizuku sources", 18, Ui.TEXT);
      shizuku = Ui.check(extra, "Enable Shizuku for this job", job.shizuku);
      apks = Ui.check(extra, "Installed application APKs", job.apks);
      data = Ui.check(extra, "Android/data", job.data);
      obb = Ui.check(extra, "Android/obb", job.obb);
      allApps = Ui.check(extra, "All apps and external app folders", job.allApps);
      Ui.hint(
          extra,
          "All apps includes third-party APKs and all external app folders. Choose apps to limit"
              + " these three sources together.");
      appSummary = Ui.hint(extra, "Selected apps: " + job.packages.length());
      chooseApps = Ui.button(extra, "Choose apps", false, v -> chooseApps());
      updateShizukuControls();
      shizuku.setOnCheckedChangeListener((button, checked) -> updateShizukuControls());
      Ui.text(extra, "Device data exports", 18, Ui.TEXT);
      for (int i = 0; i < exportOptions.length; i++) {
        String category = DeviceExport.CATEGORIES.get(i);
        boolean checked = false;
        for (int j = 0; j < job.exports.length(); j++)
          if (category.equals(job.exports.getString(j))) checked = true;
        exportOptions[i] = Ui.check(extra, DeviceExport.LABELS[i], checked);
      }
      Ui.hint(
          extra,
          "Exports require Android 12+ and Shizuku. They are ZIP files with structured records and"
              + " attachments, restored as files. Importing them into Android databases is not"
              + " implemented. Up to 512 MiB of private temporary cache is used per run and then"
              + " removed. Sources can change while apps are running.");
      Ui.hint(
          extra,
          "Grant Shizuku permission in the app's Settings. Private app data is not included;"
              + " unavailable sources fail visibly.");
      Ui.button(page, "Save job", true, v -> save());
      if (id != null)
        Ui.button(
            page,
            "Delete job",
            false,
            v ->
                new AlertDialog.Builder(this)
                    .setTitle("Delete this job?")
                    .setMessage(
                        "Remove its local settings and schedule? Backups already on PBS remain"
                            + " available.")
                    .setNegativeButton("Cancel", null)
                    .setPositiveButton(
                        "Delete",
                        (d, w) -> {
                          try {
                            JobStore.delete(this, job.id);
                            ScheduleService.configureAll(this);
                            finish();
                          } catch (Exception e) {
                            error(e);
                          }
                        })
                    .show());
      if (state != null) {
        name.setText(state.getString("name", job.name));
        store.setText(state.getString("store", job.store));
        namespace.setText(state.getString("namespace", job.namespace));
        backupID.setText(state.getString("backup_id", job.backupID(this)));
        hours.setText(state.getString("hours", String.valueOf(job.hours)));
        wifi.setChecked(state.getBoolean("wifi", job.wifi));
        charging.setChecked(state.getBoolean("charging", job.charging));
        shizuku.setChecked(state.getBoolean("shizuku", job.shizuku));
        apks.setChecked(state.getBoolean("apks", job.apks));
        data.setChecked(state.getBoolean("data", job.data));
        obb.setChecked(state.getBoolean("obb", job.obb));
        allApps.setChecked(state.getBoolean("all_apps", job.allApps));
        for (int i = 0; i < exportOptions.length; i++)
          exportOptions[i].setChecked(
              state.getBoolean("export_" + i, exportOptions[i].isChecked()));
      }
      updateShizukuControls();
    } catch (Exception e) {
      error(e);
    }
  }

  private void updateShizukuControls() {
    boolean enabled = shizuku.isChecked();
    for (android.view.View option : new android.view.View[] {apks, data, obb, allApps, appSummary})
      shizukuControl(option, enabled);
    shizukuControl(chooseApps, enabled && !choosingApps);
    for (CheckBox option : exportOptions) shizukuControl(option, enabled);
  }

  private static void shizukuControl(android.view.View option, boolean enabled) {
    if (option == null) return;
    option.setEnabled(enabled);
    option.setAlpha(enabled ? 1f : 0.4f);
  }

  private void credentialLabels() throws Exception {
    JSONObject vault = Vault.data(this);
    String id =
        job.connectionID.isEmpty() ? vault.optString("default_connection") : job.connectionID;
    String label = "Choose connection";
    if (!id.isEmpty()) label = KeyProfiles.find(vault, "connections", id).getString("name");
    connection.setText("Connection: " + label);
    encryption.setText(
        "Encryption: "
            + (job.encryptionKeyID.isEmpty()
                ? "Off"
                : KeyProfiles.find(vault, "keys", job.encryptionKeyID).getString("name")));
  }

  private void chooseCredential(boolean key) {
    try {
      JSONArray values = Vault.data(this).getJSONArray(key ? "keys" : "connections");
      String[] labels = new String[values.length() + (key ? 1 : 0)];
      if (key) labels[0] = "Off";
      for (int i = 0; i < values.length(); i++)
        labels[i + (key ? 1 : 0)] = values.getJSONObject(i).getString("name");
      if (labels.length == 0) {
        error(new Exception("Add a connection in Keys & credentials first"));
        return;
      }
      new AlertDialog.Builder(this)
          .setTitle(key ? "Backup encryption key" : "PBS connection")
          .setItems(
              labels,
              (d, w) -> {
                try {
                  String id =
                      key && w == 0 ? "" : values.getJSONObject(w - (key ? 1 : 0)).getString("id");
                  if (key) job.encryptionKeyID = id;
                  else job.connectionID = id;
                  credentialLabels();
                } catch (Exception e) {
                  error(e);
                }
              })
          .setNegativeButton("Cancel", null)
          .show();
    } catch (Exception e) {
      error(e);
    }
  }

  private void chooseApps() {
    if (choosingApps) return;
    choosingApps = true;
    updateShizukuControls();
    executor.execute(
        () -> {
          try (ShizukuBridge bridge = new ShizukuBridge(this)) {
            JSONArray inventory = new JSONArray(bridge.files().apps());
            if (inventory.length() > 500)
              throw new Exception("App picker supports at most 500 apps");
            String[] packages = new String[inventory.length()],
                labels = new String[inventory.length()];
            boolean[] checked = new boolean[inventory.length()];
            for (int i = 0; i < packages.length; i++) {
              packages[i] = inventory.getString(i);
              labels[i] = packages[i];
              try {
                android.content.pm.PackageManager pm = getPackageManager();
                labels[i] =
                    pm.getApplicationLabel(pm.getApplicationInfo(packages[i], 0))
                        + "\n"
                        + packages[i];
              } catch (Exception ignored) {
              }
              for (int j = 0; j < job.packages.length(); j++)
                if (packages[i].equals(job.packages.getString(j))) checked[i] = true;
            }
            runOnUiThread(
                () -> {
                  if (isDestroyed() || isFinishing()) return;
                  choosingApps = false;
                  updateShizukuControls();
                  new AlertDialog.Builder(this)
                      .setTitle("Choose apps and app folders")
                      .setMultiChoiceItems(
                          labels, checked, (dialog, which, value) -> checked[which] = value)
                      .setNegativeButton("Cancel", null)
                      .setPositiveButton(
                          "Use selected",
                          (dialog, which) -> {
                            job.packages = new JSONArray();
                            for (int i = 0; i < checked.length; i++)
                              if (checked[i]) job.packages.put(packages[i]);
                            allApps.setChecked(false);
                            appSummary.setText("Selected apps: " + job.packages.length());
                          })
                      .show();
                });
          } catch (Exception e) {
            runOnUiThread(
                () -> {
                  if (!isDestroyed() && !isFinishing()) {
                    choosingApps = false;
                    updateShizukuControls();
                    error(e);
                  }
                });
          }
        });
  }

  @Override
  protected void onDestroy() {
    executor.shutdownNow();
    super.onDestroy();
  }

  private void renderFolders() {
    folders.removeAllViews();
    try {
      if (job.sources.length() == 0)
        Ui.hint(folders, "No folders yet. Add DCIM for camera photos and videos.");
      for (int i = 0; i < job.sources.length(); i++) {
        final int index = i;
        JSONObject source = job.sources.getJSONObject(i);
        LinearLayout row = Ui.row(this);
        TextView label = new TextView(this);
        label.setText(source.getString("label"));
        label.setTextColor(Ui.TEXT);
        label.setTextSize(16);
        row.addView(label, new LinearLayout.LayoutParams(0, -2, 1));
        Button remove = new Button(this);
        remove.setText("Remove");
        remove.setAllCaps(false);
        remove.setOnClickListener(
            v -> {
              job.sources.remove(index);
              renderFolders();
            });
        row.addView(remove);
        folders.addView(row);
      }
    } catch (Exception e) {
      error(e);
    }
  }

  private void pick() {
    startActivityForResult(
        new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)
            .addFlags(
                Intent.FLAG_GRANT_READ_URI_PERMISSION
                    | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION
                    | Intent.FLAG_GRANT_PREFIX_URI_PERMISSION),
        100);
  }

  @Override
  protected void onActivityResult(int request, int result, Intent intent) {
    super.onActivityResult(request, result, intent);
    if (request == 101 && result == RESULT_OK && intent != null) {
      job.connectionID = intent.getStringExtra("connection_id");
      if (job.connectionID == null) job.connectionID = "";
      try {
        credentialLabels();
      } catch (Exception e) {
        error(e);
      }
      store.setText(intent.getStringExtra("store"));
      namespace.setText(intent.getStringExtra("namespace"));
      backupID.setText(intent.getStringExtra("backup_id"));
      return;
    }
    if (request != 100 || result != RESULT_OK || intent == null || intent.getData() == null) return;
    try {
      Uri uri = intent.getData();
      if ((intent.getFlags() & Intent.FLAG_GRANT_READ_URI_PERMISSION) == 0)
        throw new Exception("Folder provider did not grant read access");
      getContentResolver().takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION);
      for (int i = 0; i < job.sources.length(); i++)
        if (uri.toString().equals(job.sources.getJSONObject(i).getString("uri")))
          throw new Exception("This folder is already in the job");
      LinearLayout form = Ui.form(this);
      EditText label =
          Ui.input(form, "Folder name in backup", "Folder-" + (job.sources.length() + 1), false);
      AlertDialog dialog =
          new AlertDialog.Builder(this)
              .setTitle("Name this folder")
              .setView(form)
              .setNegativeButton("Cancel", null)
              .setPositiveButton("Add", null)
              .create();
      dialog.setOnShowListener(
          d ->
              dialog
                  .getButton(AlertDialog.BUTTON_POSITIVE)
                  .setOnClickListener(
                      v -> {
                        try {
                          String value = label.getText().toString().trim();
                          JSONArray proposed = new JSONArray(job.sources.toString());
                          proposed.put(
                              new JSONObject().put("label", value).put("uri", uri.toString()));
                          JSONArray old = job.sources;
                          job.sources = proposed;
                          try {
                            BackupJob check = new BackupJob();
                            check.sources = proposed;
                            check.validate(false);
                          } catch (Exception e) {
                            job.sources = old;
                            throw e;
                          }
                          renderFolders();
                          dialog.dismiss();
                        } catch (Exception e) {
                          label.setError(e.getMessage());
                        }
                      }));
      dialog.show();
    } catch (Exception e) {
      error(e);
    }
  }

  private void save() {
    try {
      job.name = name.getText().toString();
      job.store = store.getText().toString();
      job.namespace = namespace.getText().toString();
      job.backupID = backupID.getText().toString();
      if (job.backupID.trim().isEmpty()) throw new Exception("Enter a Backup ID");
      job.hours = Integer.parseInt(hours.getText().toString());
      job.wifi = wifi.isChecked();
      job.charging = charging.isChecked();
      job.shizuku = shizuku.isChecked();
      job.apks = apks.isChecked();
      job.data = data.isChecked();
      job.obb = obb.isChecked();
      job.allApps = allApps.isChecked();
      job.exports = new JSONArray();
      for (int i = 0; i < exportOptions.length; i++)
        if (exportOptions[i].isChecked()) job.exports.put(DeviceExport.CATEGORIES.get(i));
      job.validate(true);
      KeyProfiles.validateJob(this, job);
      JobStore.save(this, job);
      ScheduleService.configureAll(this);
      finish();
    } catch (Exception e) {
      error(e);
    }
  }

  private void error(Exception e) {
    new AlertDialog.Builder(this)
        .setMessage(e.getMessage() == null ? "Could not update job" : e.getMessage())
        .setPositiveButton("OK", null)
        .show();
  }

  @Override
  protected void onSaveInstanceState(Bundle out) {
    super.onSaveInstanceState(out);
    if (job == null || name == null) return;
    out.putString("sources", job.sources.toString());
    out.putString("name", name.getText().toString());
    out.putString("store", store.getText().toString());
    out.putString("namespace", namespace.getText().toString());
    out.putString("backup_id", backupID.getText().toString());
    out.putString("job_id", job.id);
    out.putString("connection_id", job.connectionID);
    out.putString("encryption_key_id", job.encryptionKeyID);
    out.putString("hours", hours.getText().toString());
    out.putBoolean("wifi", wifi.isChecked());
    out.putBoolean("charging", charging.isChecked());
    out.putBoolean("shizuku", shizuku.isChecked());
    out.putBoolean("apks", apks.isChecked());
    out.putBoolean("data", data.isChecked());
    out.putBoolean("obb", obb.isChecked());
    out.putBoolean("all_apps", allApps.isChecked());
    out.putString("packages", job.packages.toString());
    for (int i = 0; i < exportOptions.length; i++)
      out.putBoolean("export_" + i, exportOptions[i].isChecked());
  }
}
