package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.os.Bundle;
import android.view.WindowManager;
import android.widget.EditText;
import android.widget.LinearLayout;
import com.pbsx.core.mobilecore.Mobilecore;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.json.JSONArray;
import org.json.JSONObject;

/** Local credential and backup-key management. */
public final class KeysActivity extends Activity {
  private LinearLayout page;
  private ConnectionDialogs connections;
  private final ExecutorService executor = Executors.newSingleThreadExecutor();
  private boolean busy;

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
      Ui.button(page, "Create encryption key", true, v -> keyChoice());
      Ui.button(page, "Import encryption key", false, v -> keyDialog(true, false));
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

  private void keyChoice() {
    if (!idle()) return;
    new AlertDialog.Builder(this)
        .setTitle("Create encryption key")
        .setItems(
            new String[] {"Generate random key", "Use custom passphrase"},
            (d, w) -> keyDialog(false, w == 1))
        .setNegativeButton("Cancel", null)
        .show();
  }

  private void keyDialog(boolean importing, boolean custom) {
    if (!idle()) return;
    LinearLayout form = Ui.form(this);
    EditText name = Ui.input(form, "Key name", "Backup key", false);
    EditText secret =
        importing || custom
            ? Ui.input(
                form,
                custom ? "Custom passphrase" : "64-character hexadecimal backup key",
                "",
                true)
            : null;
    EditText confirmation = custom ? Ui.input(form, "Repeat custom passphrase", "", true) : null;
    Ui.hint(
        form,
        custom
            ? "Use a long, unique passphrase. After resetting your phone, enter exactly the same"
                + " passphrase to recreate this key. Spaces and capitalization matter. The"
                + " passphrase is not saved."
            : importing
                ? "Import a raw key from another device, or transfer keys with a complete settings"
                    + " import."
                : "A random 256-bit backup key will be generated on this device.");
    AlertDialog dialog =
        new AlertDialog.Builder(this)
            .setTitle(
                custom
                    ? "Custom backup key"
                    : importing ? "Import encryption key" : "Generate encryption key")
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
                        String label = name.getText().toString();
                        KeyProfiles.label(label);
                        String value = secret == null ? "" : secret.getText().toString();
                        if (custom && value.isEmpty()) {
                          secret.setError("Enter a passphrase");
                          return;
                        }
                        if (custom && !value.equals(confirmation.getText().toString())) {
                          confirmation.setError("Passphrases do not match");
                          return;
                        }
                        if (secret != null) secret.setText("");
                        if (confirmation != null) confirmation.setText("");
                        dialog.dismiss();
                        background(
                            "Saving encryption key…",
                            () -> {
                              String raw =
                                  custom
                                      ? Mobilecore.deriveEncryptionKey(value)
                                      : importing ? value : Mobilecore.generateEncryptionKey();
                              KeyProfiles.addKey(this, label, raw);
                              return "";
                            });
                      } catch (Exception e) {
                        name.setError(e.getMessage());
                      }
                    }));
    dialog.show();
  }

  @Override
  protected void onDestroy() {
    executor.shutdown();
    super.onDestroy();
  }
}
