package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.view.WindowManager;
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

/** Local credential/key management and explicit portable settings import/export. */
public final class KeysActivity extends Activity {
  private LinearLayout page;
  private ConnectionDialogs connections;
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private boolean busy;
  private byte[] pendingExport;
  private String pendingImport;

  @Override
  public void onCreate(Bundle state) {
    super.onCreate(state);
    getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
    page = Ui.scroll(Ui.screen(this, "Keys & credentials"));
    connections = new ConnectionDialogs(this, this::background, this::render);
    render();
  }

  private void message(String value) {
    if (!isDestroyed() && !isFinishing())
      new AlertDialog.Builder(this).setMessage(value).setPositiveButton("OK", null).show();
  }

  private boolean idle() {
    if (busy || Operations.running()) {
      message("Wait for the active operation or cancel it first");
      return false;
    }
    return true;
  }

  private void background(String label, ConnectionDialogs.Action action) {
    if (!idle()) return;
    busy = true;
    page.removeAllViews();
    Ui.title(page, label);
    executor.execute(
        () -> {
          String outcome;
          try {
            outcome = action.run();
          } catch (Exception e) {
            outcome = e.getMessage() == null ? "Operation failed" : e.getMessage();
          }
          final String result = outcome;
          runOnUiThread(
              () -> {
                busy = false;
                if (isDestroyed() || isFinishing()) return;
                render();
                if (result != null && !result.isEmpty()) message(result);
              });
        });
  }

  private void render() {
    if (busy || page == null || isDestroyed()) return;
    page.removeAllViews();
    try {
      JSONObject vault = Vault.data(this);
      JSONArray profiles = vault.getJSONArray("connections"), keys = vault.getJSONArray("keys");
      Ui.title(page, "PBS connections");
      Ui.hint(
          page,
          "Jobs select saved connections independently. Enrollment creates a normal saved entry."
              + " Removing it here does not revoke its PBS token.");
      for (int i = 0; i < profiles.length(); i++) {
        JSONObject item = profiles.getJSONObject(i), cfg = item.getJSONObject("config");
        String id = item.getString("id");
        LinearLayout card = Ui.card(page);
        Ui.text(
            card,
            item.getString("name")
                + (id.equals(vault.optString("default_connection"))
                    ? " · default for Restore"
                    : ""),
            19,
            Ui.TEXT);
        Ui.hint(
            card,
            cfg.optString("server")
                + "\n"
                + (cfg.optString("storage_auth_id").isEmpty()
                    ? cfg.optString("auth_id")
                    : cfg.optString("storage_auth_id"))
                + "\nUsed by "
                + uses("connections", id)
                + " jobs");
        Ui.button(
            card,
            "Use as default for Restore",
            false,
            v -> {
              try {
                if (!idle()) return;
                KeyProfiles.setDefault(this, id);
                render();
              } catch (Exception e) {
                message(e.getMessage());
              }
            });
        Ui.button(card, "Change storage access key", false, v -> connections.storage(id));
        entryActions(card, "connections", item);
      }
      Ui.button(page, "Enroll with PBS-X code", true, v -> connections.enroll());
      Ui.button(page, "Add existing API key", false, v -> connections.manual());
      Ui.title(page, "Backup encryption keys");
      Ui.hint(
          page,
          "Keys encrypt backup contents before upload. Export settings and keep a recovery copy"
              + " elsewhere before relying on an encrypted backup.");
      for (int i = 0; i < keys.length(); i++) {
        JSONObject item = keys.getJSONObject(i);
        String id = item.getString("id");
        LinearLayout card = Ui.card(page);
        Ui.text(card, item.getString("name"), 19, Ui.TEXT);
        Ui.hint(
            card,
            "Fingerprint\n"
                + item.getString("fingerprint")
                + "\nUsed by "
                + uses("keys", id)
                + " jobs");
        entryActions(card, "keys", item);
      }
      Ui.button(page, "Create encryption key", true, v -> keyDialog(false));
      Ui.button(page, "Import encryption key", false, v -> keyDialog(true));
      Ui.title(page, "Settings export & import");
      Ui.hint(
          page,
          "Includes every saved job, schedule, source selection, preference, connection, API"
              + " secret, encryption key and activity entry. Folder and Shizuku permissions must be"
              + " granted on the destination device. Imported schedules stay paused until reviewed"
              + " in Settings.");
      Ui.button(page, "Export all settings", true, v -> exportChoice());
      Ui.button(
          page,
          "Import all settings",
          false,
          v -> {
            if (idle())
              startActivityForResult(
                  new Intent(Intent.ACTION_OPEN_DOCUMENT)
                      .setType("application/json")
                      .addCategory(Intent.CATEGORY_OPENABLE),
                  402);
          });
    } catch (Exception e) {
      Ui.hint(page, e.getMessage());
    }
  }

  private int uses(String kind, String id) throws Exception {
    int count = 0;
    String selected = KeyProfiles.selected(this);
    for (BackupJob job : JobStore.all(this))
      if (id.equals(
          kind.equals("keys")
              ? job.encryptionKeyID
              : job.connectionID.isEmpty() ? selected : job.connectionID)) count++;
    return count;
  }

  private void entryActions(LinearLayout card, String kind, JSONObject item) throws Exception {
    String id = item.getString("id");
    Ui.button(
        card,
        "Rename",
        false,
        v -> {
          if (!idle()) return;
          LinearLayout form = Ui.form(this);
          EditText name = Ui.input(form, "Name", item.optString("name"), false);
          AlertDialog dialog =
              new AlertDialog.Builder(this)
                  .setTitle("Rename saved entry")
                  .setView(form)
                  .setNegativeButton("Cancel", null)
                  .setPositiveButton("Save", null)
                  .create();
          dialog.setOnShowListener(
              d ->
                  dialog
                      .getButton(-1)
                      .setOnClickListener(
                          view -> {
                            try {
                              if (!idle()) return;
                              KeyProfiles.rename(this, kind, id, name.getText().toString());
                              dialog.dismiss();
                              render();
                            } catch (Exception e) {
                              name.setError(e.getMessage());
                            }
                          }));
          dialog.show();
        });
    Ui.button(
        card,
        "Remove from this device",
        false,
        v -> {
          if (!idle()) return;
          new AlertDialog.Builder(this)
              .setTitle("Remove saved entry?")
              .setMessage(
                  kind.equals("keys")
                      ? "Historical snapshots may still need this key, even if no job uses it. Keep"
                          + " an exported recovery copy. This does not change backups already"
                          + " stored on PBS."
                      : "This removes local access settings. It does not revoke the API token on"
                          + " PBS.")
              .setNegativeButton("Cancel", null)
              .setPositiveButton(
                  "Remove",
                  (d, w) -> {
                    try {
                      KeyProfiles.remove(this, kind, id);
                      render();
                    } catch (Exception e) {
                      message(e.getMessage());
                    }
                  })
              .show();
        });
  }

  private void keyDialog(boolean importing) {
    if (!idle()) return;
    LinearLayout form = Ui.form(this);
    EditText name = Ui.input(form, "Key name", "Backup key", false);
    EditText secret =
        importing ? Ui.input(form, "64-character hexadecimal backup key", "", true) : null;
    Ui.hint(
        form,
        importing
            ? "Import a key from another PBS-X device. Complete encrypted settings exports can also"
                + " transfer keys."
            : "A random 256-bit backup key will be generated on this device.");
    AlertDialog dialog =
        new AlertDialog.Builder(this)
            .setTitle(importing ? "Import encryption key" : "Create encryption key")
            .setView(Ui.wrapped(form))
            .setNegativeButton("Cancel", null)
            .setPositiveButton("Save", null)
            .create();
    dialog.setOnShowListener(
        d ->
            dialog
                .getButton(-1)
                .setOnClickListener(
                    v -> {
                      try {
                        KeyProfiles.addKey(
                            this,
                            name.getText().toString(),
                            importing
                                ? secret.getText().toString()
                                : Mobilecore.generateEncryptionKey());
                        if (secret != null) secret.setText("");
                        dialog.dismiss();
                        render();
                      } catch (Exception e) {
                        name.setError(e.getMessage());
                      }
                    }));
    dialog.show();
  }

  private void exportChoice() {
    if (!idle()) return;
    new AlertDialog.Builder(this)
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
    LinearLayout form = Ui.form(this);
    EditText password =
        Ui.input(form, importing ? "Export password" : "Password for this export", "", true);
    password.setSaveEnabled(false);
    AlertDialog dialog =
        new AlertDialog.Builder(this)
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
  }

  private void prepareExport(String password) {
    background(
        "Preparing settings export…",
        () -> {
          String plain = SettingsTransfer.exportJSON(this);
          String raw = password.isEmpty() ? plain : Mobilecore.encryptSettings(plain, password);
          pendingExport = raw.getBytes(StandardCharsets.UTF_8);
          runOnUiThread(
              () -> {
                if (!isDestroyed() && !isFinishing())
                  startActivityForResult(
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

  @Override
  protected void onActivityResult(int request, int result, Intent intent) {
    super.onActivityResult(request, result, intent);
    if (result != RESULT_OK || intent == null || intent.getData() == null) {
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
            try (OutputStream out = getContentResolver().openOutputStream(uri, "wt")) {
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
            try (InputStream in = getContentResolver().openInputStream(uri)) {
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
            runOnUiThread(
                () -> {
                  if (isDestroyed() || isFinishing()) return;
                  if ("pbsx-settings-encrypted".equals(header.optString("format"))) password(true);
                  else decodeImport("");
                });
            return "";
          });
    }
  }

  private void decodeImport(String password) {
    // The file-picker callback may still be unwinding its worker; post once to the UI queue.
    if (busy) {
      page.post(() -> decodeImport(password));
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
          runOnUiThread(
              () -> {
                if (isDestroyed() || isFinishing()) return;
                new AlertDialog.Builder(this)
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
                                SettingsTransfer.importJSON(this, validated);
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
  protected void onDestroy() {
    pendingImport = null;
    if (pendingExport != null) java.util.Arrays.fill(pendingExport, (byte) 0);
    pendingExport = null;
    executor.shutdown();
    super.onDestroy();
  }
}
