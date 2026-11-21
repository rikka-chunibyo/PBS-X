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
  private EditText name, store, namespace, backupID, hours, excludes;
  private CheckBox wifi, charging, shizuku;
  private CheckBox calendar;
  private EditText scheduleTimes, scheduleZone;
  private LinearLayout calendarOptions;
  private final CheckBox[] scheduleDays = new CheckBox[7];
  private final CheckBox[] exportOptions = new CheckBox[DeviceExport.CATEGORIES.size()];
  private final java.util.concurrent.ExecutorService executor =
      java.util.concurrent.Executors.newSingleThreadExecutor();
  private final AppSelectionCard[] appCards = new AppSelectionCard[3];
  private LinearLayout folders, shizukuFolders;
  private Button addShizukuFolder;
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
        job.shizukuFolders =
            new JSONArray(state.getString("shizuku_folders", job.shizukuFolders.toString()));
        job.packages = new JSONArray(state.getString("packages", job.packages.toString()));
        job.appSelections =
            new JSONObject(state.getString("app_selections", job.appSelections.toString()));
        job.apks = state.getBoolean("apks", job.apks);
        job.data = state.getBoolean("data", job.data);
        job.obb = state.getBoolean("obb", job.obb);
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
      LinearLayout exclusions = Ui.card(page);
      Ui.text(exclusions, "Exclude files & directories", 18, Ui.TEXT);
      excludes =
          Ui.input(
              exclusions,
              "One backup-relative path or pattern per line",
              BackupExclusions.text(job.excludes),
              false);
      excludes.setSingleLine(false);
      excludes.setMinLines(3);
      excludes.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE);
      excludes.setContentDescription("Backup exclusions");
      Ui.hint(
          exclusions,
          "For example DCIM/.thumbnails excludes that folder and everything inside it. Use the"
              + " folder labels shown in this job. * matches within one name, ? matches one"
              + " character, and ** matches any depth; **/*.tmp excludes temporary files anywhere."
              + " Exclusions also apply to Shizuku sources.");
      LinearLayout options = Ui.card(page);
      Ui.text(options, "Schedule & connection", 18, Ui.TEXT);
      hours =
          Ui.input(options, "Run every … hours · 0 means manual", String.valueOf(job.hours), false);
      hours.setInputType(InputType.TYPE_CLASS_NUMBER);
      calendar = Ui.check(options, "Use specific times and weekdays", job.calendar);
      calendarOptions = Ui.column(this);
      options.addView(calendarOptions);
      scheduleTimes =
          Ui.input(
              calendarOptions, "Daily times · 24-hour, comma-separated", job.scheduleTimes, false);
      String[] weekdays = {
        "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"
      };
      for (int i = 0; i < weekdays.length; i++)
        scheduleDays[i] =
            Ui.check(calendarOptions, weekdays[i], (job.scheduleDays & (1 << i)) != 0);
      scheduleZone =
          Ui.input(
              calendarOptions, "Time zone · blank follows the device", job.scheduleZone, false);
      calendar.setOnCheckedChangeListener((button, checked) -> updateScheduleControls());
      updateScheduleControls();
      wifi = Ui.check(options, "Wi-Fi only", job.wifi);
      charging = Ui.check(options, "Require charging for scheduled runs", job.charging);
      Ui.hint(
          options,
          "Scheduled times are earliest start times; Android may defer them until charging and"
              + " Wi-Fi requirements are met. Each job keeps one pending occurrence and transfers"
              + " run one at a time. Local times adjust for daylight saving.");
      LinearLayout extra = Ui.card(page);
      Ui.text(extra, "Optional Shizuku sources", 18, Ui.TEXT);
      shizuku = Ui.check(extra, "Enable Shizuku for this job", job.shizuku);
      Ui.text(extra, "Shared-storage folders", 18, Ui.TEXT);
      Ui.hint(
          extra,
          "Select Download or another internal shared folder through Shizuku. These folders are"
              + " independent of the app selection below. SD-card folders use the normal Add folder"
              + " button.");
      shizukuFolders = Ui.column(this);
      extra.addView(shizukuFolders);
      addShizukuFolder =
          Ui.button(
              extra,
              "+ Add folder via Shizuku",
              false,
              v -> new ShizukuFolderPicker(this, executor, path -> nameFolder(null, path)).show());
      renderShizukuFolders();
      appCards[0] =
          new AppSelectionCard(this, page, job, "apks", "Installed application APKs", executor);
      appCards[1] = new AppSelectionCard(this, page, job, "data", "Android/data", executor);
      appCards[2] = new AppSelectionCard(this, page, job, "obb", "Android/obb", executor);
      shizuku.setOnCheckedChangeListener((button, checked) -> updateShizukuControls());
      extra = Ui.card(page);
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
                            KeyProfiles.deleteJob(this, job.id);
                            ScheduleService.configureAll(this);
                            finish();
                          } catch (Exception e) {
                            error(e);
                          }
                        })
                    .show());
      if (state != null) {
        name.setText(state.getString("name", job.name));
        excludes.setText(state.getString("excludes", BackupExclusions.text(job.excludes)));
        store.setText(state.getString("store", job.store));
        namespace.setText(state.getString("namespace", job.namespace));
        backupID.setText(state.getString("backup_id", job.backupID(this)));
        hours.setText(state.getString("hours", String.valueOf(job.hours)));
        calendar.setChecked(state.getBoolean("calendar", job.calendar));
        scheduleTimes.setText(state.getString("schedule_times", job.scheduleTimes));
        scheduleZone.setText(state.getString("schedule_zone", job.scheduleZone));
        for (int i = 0; i < scheduleDays.length; i++)
          scheduleDays[i].setChecked(
              (state.getInt("schedule_days", job.scheduleDays) & (1 << i)) != 0);
        wifi.setChecked(state.getBoolean("wifi", job.wifi));
        charging.setChecked(state.getBoolean("charging", job.charging));
        shizuku.setChecked(state.getBoolean("shizuku", job.shizuku));
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
    for (AppSelectionCard card : appCards) if (card != null) card.setEnabled(enabled);
    shizukuControl(addShizukuFolder, enabled);
    if (shizukuFolders != null) {
      for (int i = 0; i < shizukuFolders.getChildCount(); i++) {
        android.view.View row = shizukuFolders.getChildAt(i);
        shizukuControl(row, enabled);
        if (row instanceof android.view.ViewGroup) {
          android.view.ViewGroup group = (android.view.ViewGroup) row;
          for (int j = 0; j < group.getChildCount(); j++) group.getChildAt(j).setEnabled(enabled);
        }
      }
    }
    for (CheckBox option : exportOptions) shizukuControl(option, enabled);
  }

  private void updateScheduleControls() {
    boolean enabled = calendar.isChecked();
    calendarOptions.setVisibility(enabled ? android.view.View.VISIBLE : android.view.View.GONE);
    shizukuControl(hours, !enabled);
    shizukuControl(scheduleTimes, enabled);
    shizukuControl(scheduleZone, enabled);
    for (CheckBox day : scheduleDays) shizukuControl(day, enabled);
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

  private void renderShizukuFolders() {
    shizukuFolders.removeAllViews();
    try {
      if (job.shizukuFolders.length() == 0) Ui.hint(shizukuFolders, "No Shizuku folders selected.");
      for (int i = 0; i < job.shizukuFolders.length(); i++) {
        final int index = i;
        JSONObject folder = job.shizukuFolders.getJSONObject(i);
        LinearLayout row = Ui.row(this);
        TextView label =
            Ui.text(row, folder.getString("label") + "\n" + folder.getString("path"), 16, Ui.TEXT);
        label.setLayoutParams(new LinearLayout.LayoutParams(0, -2, 1));
        Ui.button(
            row,
            "Remove",
            false,
            v -> {
              job.shizukuFolders.remove(index);
              renderShizukuFolders();
              updateShizukuControls();
            });
        shizukuFolders.addView(row);
      }
    } catch (Exception e) {
      error(e);
    }
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
      nameFolder(uri, null);
    } catch (Exception e) {
      error(e);
    }
  }

  private void nameFolder(Uri uri, String sharedPath) {
    if (sharedPath != null) {
      showFolderName(uri, sharedPath, sharedPath.substring(sharedPath.lastIndexOf('/') + 1));
      return;
    }
    executor.execute(
        () -> {
          String folderName = "";
          try {
            folderName = new DocumentTree(getContentResolver(), uri).stat(".").getString("name");
          } catch (Exception ignored) {
            // A provider may omit its display name; the label remains editable.
          }
          String suggested = folderName;
          runOnUiThread(
              () -> {
                if (!isDestroyed() && !isFinishing()) showFolderName(uri, null, suggested);
              });
        });
  }

  private void showFolderName(Uri uri, String sharedPath, String suggested) {
    try {
      boolean shared = sharedPath != null;
      if (shared) {
        SharedStorage.validate(sharedPath, false);
        for (int i = 0; i < job.shizukuFolders.length(); i++)
          if (sharedPath.equals(job.shizukuFolders.getJSONObject(i).getString("path")))
            throw new Exception("This Shizuku folder is already in the job");
      }
      LinearLayout form = Ui.form(this);
      EditText label =
          Ui.input(
              form,
              "Folder name in backup",
              suggested.trim().isEmpty() ? "Folder-" + (job.sources.length() + 1) : suggested,
              false);
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
                          JSONArray proposed =
                              new JSONArray((shared ? job.shizukuFolders : job.sources).toString());
                          proposed.put(
                              new JSONObject()
                                  .put("label", value)
                                  .put(
                                      shared ? "path" : "uri",
                                      shared ? sharedPath : uri.toString()));
                          BackupJob check = BackupJob.from(job.json());
                          if (shared) check.shizukuFolders = proposed;
                          else check.sources = proposed;
                          check.shizuku = shizuku.isChecked();
                          check.exports = new JSONArray();
                          for (int i = 0; i < exportOptions.length; i++)
                            if (exportOptions[i].isChecked())
                              check.exports.put(DeviceExport.CATEGORIES.get(i));
                          check.validate(false);
                          if (shared) job.shizukuFolders = proposed;
                          else job.sources = proposed;
                          renderFolders();
                          renderShizukuFolders();
                          updateShizukuControls();
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
      job.excludes = BackupExclusions.parse(excludes.getText().toString());
      job.store = store.getText().toString();
      job.namespace = namespace.getText().toString();
      job.backupID = backupID.getText().toString();
      if (job.backupID.trim().isEmpty()) throw new Exception("Enter a Backup ID");
      job.hours = Integer.parseInt(hours.getText().toString());
      job.calendar = calendar.isChecked();
      job.scheduleTimes = scheduleTimes.getText().toString().trim();
      job.scheduleZone = scheduleZone.getText().toString().trim();
      job.scheduleDays = 0;
      for (int i = 0; i < scheduleDays.length; i++)
        if (scheduleDays[i].isChecked()) job.scheduleDays |= 1 << i;
      job.wifi = wifi.isChecked();
      job.charging = charging.isChecked();
      job.shizuku = shizuku.isChecked();
      for (AppSelectionCard card : appCards) card.save();
      job.exports = new JSONArray();
      for (int i = 0; i < exportOptions.length; i++)
        if (exportOptions[i].isChecked()) job.exports.put(DeviceExport.CATEGORIES.get(i));
      job.validate(true);
      KeyProfiles.saveJob(this, job);
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
    out.putString("shizuku_folders", job.shizukuFolders.toString());
    out.putString("name", name.getText().toString());
    out.putString("excludes", excludes.getText().toString());
    out.putString("store", store.getText().toString());
    out.putString("namespace", namespace.getText().toString());
    out.putString("backup_id", backupID.getText().toString());
    out.putString("job_id", job.id);
    out.putString("connection_id", job.connectionID);
    out.putString("encryption_key_id", job.encryptionKeyID);
    out.putString("hours", hours.getText().toString());
    out.putBoolean("calendar", calendar.isChecked());
    out.putString("schedule_times", scheduleTimes.getText().toString());
    out.putString("schedule_zone", scheduleZone.getText().toString());
    int days = 0;
    for (int i = 0; i < scheduleDays.length; i++) if (scheduleDays[i].isChecked()) days |= 1 << i;
    out.putInt("schedule_days", days);
    out.putBoolean("wifi", wifi.isChecked());
    out.putBoolean("charging", charging.isChecked());
    out.putBoolean("shizuku", shizuku.isChecked());
    out.putBoolean("apks", job.apks);
    out.putBoolean("data", job.data);
    out.putBoolean("obb", job.obb);
    try {
      for (AppSelectionCard card : appCards) if (card != null) card.save();
      out.putString("app_selections", job.appSelections.toString());
    } catch (Exception ignored) {
    }
    out.putString("packages", job.packages.toString());
    for (int i = 0; i < exportOptions.length; i++)
      out.putBoolean("export_" + i, exportOptions[i].isChecked());
  }
}
