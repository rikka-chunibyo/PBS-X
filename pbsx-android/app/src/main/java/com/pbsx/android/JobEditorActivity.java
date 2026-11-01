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
  private EditText name, store, namespace, hours;
  private CheckBox wifi, charging, shizuku, apks, data, obb;
  private LinearLayout folders;

  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    try {
      String id = getIntent().getStringExtra("job_id");
      job = id == null ? new BackupJob() : JobStore.get(this, id);
      if (state != null) {
        job.sources = new JSONArray(state.getString("sources", "[]"));
      }
      LinearLayout root = Ui.screen(this, id == null ? "New backup job" : "Edit backup job"),
          page = Ui.scroll(root);
      LinearLayout basic = Ui.card(page);
      Ui.title(basic, "Job details");
      name = Ui.input(basic, "Name", job.name, false);
      LinearLayout dest = Ui.card(page);
      Ui.text(dest, "Destination", 18, Ui.TEXT);
      store = Ui.input(dest, "Datastore", job.store, false);
      namespace = Ui.input(dest, "Namespace · leave blank for root", job.namespace, false);
      Ui.hint(
          dest,
          "This host must have an enabled server job authorizing this destination. Jobs here select"
              + " your phone's folders and schedule.");
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
        hours.setText(state.getString("hours", String.valueOf(job.hours)));
        wifi.setChecked(state.getBoolean("wifi", job.wifi));
        charging.setChecked(state.getBoolean("charging", job.charging));
        shizuku.setChecked(state.getBoolean("shizuku", job.shizuku));
        apks.setChecked(state.getBoolean("apks", job.apks));
        data.setChecked(state.getBoolean("data", job.data));
        obb.setChecked(state.getBoolean("obb", job.obb));
      }
    } catch (Exception e) {
      error(e);
    }
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
      job.hours = Integer.parseInt(hours.getText().toString());
      job.wifi = wifi.isChecked();
      job.charging = charging.isChecked();
      job.shizuku = shizuku.isChecked();
      job.apks = apks.isChecked();
      job.data = data.isChecked();
      job.obb = obb.isChecked();
      job.validate(true);
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
    out.putString("hours", hours.getText().toString());
    out.putBoolean("wifi", wifi.isChecked());
    out.putBoolean("charging", charging.isChecked());
    out.putBoolean("shizuku", shizuku.isChecked());
    out.putBoolean("apks", apks.isChecked());
    out.putBoolean("data", data.isChecked());
    out.putBoolean("obb", obb.isChecked());
  }
}
