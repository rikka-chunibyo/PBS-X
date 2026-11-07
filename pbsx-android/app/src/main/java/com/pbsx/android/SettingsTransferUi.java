package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.Uri;
import android.widget.EditText;
import android.widget.LinearLayout;
import com.pbsx.core.mobilecore.Mobilecore;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.json.JSONArray;
import org.json.JSONObject;

/** Settings-page document picker and password UI. Passwords remain transient. */
final class SettingsTransferUi implements AutoCloseable {
  private final Activity a;
  private final Runnable changed;
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private boolean busy, closed;
  private String phase = "";
  private byte[] pendingExport;
  private String pendingImport;

  SettingsTransferUi(Activity activity, Runnable changed) {
    a = activity;
    this.changed = changed;
  }

  boolean running() {
    return busy;
  }

  private void message(String value) {
    if (!closed && !a.isDestroyed() && !a.isFinishing())
      new AlertDialog.Builder(a).setMessage(value).setPositiveButton("OK", null).show();
  }

  private boolean idle() {
    if (closed) return false;
    if (busy || Operations.running()) {
      message("Wait for the active operation or cancel it first");
      return false;
    }
    return true;
  }

  void render(LinearLayout page) {
    LinearLayout card = Ui.card(page);
    Ui.text(card, "Settings export & import", 19, Ui.TEXT);
    Ui.hint(
        card,
        "Includes all saved jobs, schedules, preferences, credentials, encryption keys and activity"
            + " history. Imported schedules stay paused until permissions are reviewed.");
    if (busy) Ui.hint(card, phase);
    Ui.button(card, "Export all settings", true, v -> exportChoice()).setEnabled(!busy);
    Ui.button(
            card,
            "Import all settings",
            false,
            v -> {
              if (idle())
                a.startActivityForResult(
                    new Intent(Intent.ACTION_OPEN_DOCUMENT)
                        .setType("application/json")
                        .addCategory(Intent.CATEGORY_OPENABLE),
                    402);
            })
        .setEnabled(!busy);
  }

  private void background(String label, ConnectionDialogs.Action action) {
    if (!idle()) return;
    busy = true;
    phase = label;
    changed.run();
    executor.execute(
        () -> {
          String outcome;
          try {
            outcome = action.run();
          } catch (Exception e) {
            outcome = e.getMessage() == null ? "Operation failed" : e.getMessage();
          }
          final String result = outcome;
          a.runOnUiThread(
              () -> {
                busy = false;
                phase = "";
                if (closed || a.isDestroyed() || a.isFinishing()) return;
                changed.run();
                if (result != null && !result.isEmpty()) message(result);
              });
        });
  }

  void exportChoice() {
    if (!idle()) return;
    new AlertDialog.Builder(a)
        .setTitle("Encrypt exported JSON?")
        .setMessage(
            "The export includes all saved API credentials and backup encryption keys. A password"
                + " protects this export only and is never saved.")
        .setNegativeButton("Cancel", null)
        .setNeutralButton("Plain JSON", (d, w) -> prepareExport(""))
        .setPositiveButton("Use password", (d, w) -> password(false))
        .show();
  }

  private void password(boolean importing) {
    LinearLayout form = Ui.form(a);
    EditText password =
        Ui.input(form, importing ? "Export password" : "Password for this export", "", true);
    password.setSaveEnabled(false);
    AlertDialog dialog =
        new AlertDialog.Builder(a)
            .setTitle(importing ? "Decrypt settings export" : "Encrypt settings export")
            .setView(form)
            .setNegativeButton("Cancel", (d, w) -> pendingImport = null)
            .setPositiveButton(importing ? "Unlock" : "Export", null)
            .create();
    dialog.setOnShowListener(
        d ->
            dialog
                .getButton(-1)
                .setOnClickListener(
                    v -> {
                      String value = password.getText().toString();
                      if (value.isEmpty()) {
                        password.setError("Enter a password");
                        return;
                      }
                      password.setText("");
                      dialog.dismiss();
                      if (importing) decodeImport(value);
                      else prepareExport(value);
                    }));
    dialog.show();
    dialog.getWindow().addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE);
  }

  private void prepareExport(String password) {
    background(
        "Preparing settings export…",
        () -> {
          String plain = SettingsTransfer.exportJSON(a);
          String raw = password.isEmpty() ? plain : Mobilecore.encryptSettings(plain, password);
          pendingExport = raw.getBytes(StandardCharsets.UTF_8);
          a.runOnUiThread(
              () -> {
                if (!a.isDestroyed() && !a.isFinishing())
                  a.startActivityForResult(
                      new Intent(Intent.ACTION_CREATE_DOCUMENT)
                          .setType("application/json")
                          .addCategory(Intent.CATEGORY_OPENABLE)
                          .putExtra(
                              Intent.EXTRA_TITLE,
                              password.isEmpty()
                                  ? "pbsx-settings.json"
                                  : "pbsx-settings-encrypted.json"),
                      401);
              });
          return "";
        });
  }

  void activityResult(int request, int result, Intent intent) {
    if (result != Activity.RESULT_OK || intent == null || intent.getData() == null) {
      pendingExport = null;
      pendingImport = null;
      return;
    }
    Uri uri = intent.getData();
    if (request == 401) {
      byte[] bytes = pendingExport;
      pendingExport = null;
      if (bytes == null) {
        message("Export was cancelled when the screen was recreated; export again");
        return;
      }
      background(
          "Writing settings export…",
          () -> {
            try (OutputStream out = a.getContentResolver().openOutputStream(uri, "wt")) {
              if (out == null) throw new Exception("Cannot open export destination");
              out.write(bytes);
            } finally {
              java.util.Arrays.fill(bytes, (byte) 0);
            }
            return "All settings and keys exported";
          });
    } else if (request == 402) {
      background(
          "Reading settings export…",
          () -> {
            byte[] bytes;
            try (InputStream in = a.getContentResolver().openInputStream(uri)) {
              if (in == null) throw new Exception("Cannot open settings export");
              java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
              byte[] block = new byte[8192];
              int n;
              while ((n = in.read(block)) != -1) {
                if (out.size() + n > SettingsTransfer.LIMIT * 2)
                  throw new Exception("Settings export is too large");
                out.write(block, 0, n);
              }
              bytes = out.toByteArray();
            }
            pendingImport = new String(bytes, StandardCharsets.UTF_8);
            JSONObject header = new JSONObject(pendingImport);
            a.runOnUiThread(
                () -> {
                  if (a.isDestroyed() || a.isFinishing()) return;
                  if ("pbsx-settings-encrypted".equals(header.optString("format"))) password(true);
                  else decodeImport("");
                });
            return "";
          });
    }
  }

  private void decodeImport(String password) {
    if (closed || a.isDestroyed() || a.isFinishing()) return;
    // The file-picker callback may still be unwinding its worker; post once to the UI queue.
    if (busy) {
      a.getWindow().getDecorView().post(() -> decodeImport(password));
      return;
    }
    background(
        "Validating settings…",
        () -> {
          String raw = pendingImport;
          if (raw == null) throw new Exception("Choose a settings export first");
          if (!password.isEmpty()) raw = Mobilecore.decryptSettings(raw, password);
          JSONObject value = SettingsTransfer.validate(raw);
          final String validated = raw;
          int jobs =
              new JSONArray(
                      value
                                  .getJSONObject("preferences")
                                  .getJSONObject("jobs-v1")
                                  .optJSONObject("jobs")
                              == null
                          ? "[]"
                          : value
                              .getJSONObject("preferences")
                              .getJSONObject("jobs-v1")
                              .getJSONObject("jobs")
                              .getString("value"))
                  .length();
          int profiles = value.getJSONObject("vault").getJSONArray("connections").length(),
              keys = value.getJSONObject("vault").getJSONArray("keys").length();
          a.runOnUiThread(
              () -> {
                if (a.isDestroyed() || a.isFinishing()) return;
                new AlertDialog.Builder(a)
                    .setTitle("Replace all saved app settings?")
                    .setMessage(
                        jobs
                            + " jobs, "
                            + profiles
                            + " connections and "
                            + keys
                            + " encryption keys, plus preferences and activity history. Existing"
                            + " local settings will be replaced. Imported schedules stay paused;"
                            + " review folder and Shizuku permissions before resuming.")
                    .setNegativeButton("Cancel", (d, w) -> pendingImport = null)
                    .setPositiveButton(
                        "Import",
                        (d, w) -> {
                          pendingImport = null;
                          background(
                              "Importing settings…",
                              () -> {
                                SettingsTransfer.importJSON(a, validated);
                                return "Settings imported. Review permissions, then resume"
                                    + " schedules in Settings when ready.";
                              });
                        })
                    .show();
              });
          return "";
        });
  }

  @Override
  public void close() {
    closed = true;
    pendingImport = null;
    if (pendingExport != null) java.util.Arrays.fill(pendingExport, (byte) 0);
    pendingExport = null;
    executor.shutdown();
  }
}
