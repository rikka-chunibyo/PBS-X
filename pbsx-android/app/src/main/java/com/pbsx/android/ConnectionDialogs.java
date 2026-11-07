package com.pbsx.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.os.Build;
import android.widget.EditText;
import android.widget.LinearLayout;
import com.pbsx.core.mobilecore.Mobilecore;
import org.json.JSONObject;

/** Enrollment and credentials stay separate from local job configuration. */
final class ConnectionDialogs {
  interface Action {
    String run() throws Exception;
  }

  interface Background {
    void run(String label, Action action);
  }

  private final Activity a;
  private final Background background;
  private final Runnable changed;

  ConnectionDialogs(Activity activity, Background background, Runnable changed) {
    a = activity;
    this.background = background;
    this.changed = changed;
  }

  private boolean idle() {
    if (Operations.running()) {
      message("Wait for the active transfer or cancel it before changing the connection.");
      return false;
    }
    return true;
  }

  private void message(String text) {
    new AlertDialog.Builder(a).setMessage(text).setPositiveButton("OK", null).show();
  }

  void enroll() {
    if (!idle()) return;
    LinearLayout f = Ui.form(a);
    Ui.hint(
        f,
        "Create a host and enrollment code in PBS-X → Hosts. Its enabled server job authorizes the"
            + " datastore and namespace used by your local jobs.");
    EditText name = Ui.input(f, "Connection name", "My PBS", false);
    EditText code = Ui.input(f, "PBS-X enrollment code", "", true);
    new AlertDialog.Builder(a)
        .setTitle("Connect with enrollment code")
        .setView(Ui.wrapped(f))
        .setNegativeButton("Cancel", null)
        .setPositiveButton(
            "Enroll",
            (d, w) -> {
              String profileName = name.getText().toString();
              String value = code.getText().toString().trim();
              code.setText("");
              background.run(
                  "Enrolling…",
                  () -> {
                    KeyProfiles.label(profileName);
                    String config =
                        Mobilecore.enroll(value, Build.MANUFACTURER + " " + Build.MODEL);
                    KeyProfiles.addConnection(a, profileName, config);
                    return "Connected. Your backup jobs can use an authorized datastore and"
                        + " namespace.";
                  });
            })
        .show();
  }

  void manual() {
    if (!idle()) return;
    LinearLayout f = Ui.form(a);
    Ui.hint(
        f,
        "Connect using a normal PBS API token and the server's SHA-256 certificate fingerprint."
            + " Each saved connection can be selected independently by jobs.");
    EditText name = Ui.input(f, "Connection name", "My PBS", false);
    EditText server = Ui.input(f, "HTTPS server", "https://", false),
        host = Ui.input(f, "Device ID", "", false),
        pin = Ui.input(f, "SHA-256 certificate fingerprint", "", false),
        id = Ui.input(f, "Auth ID · user@realm!token", "", false),
        secret = Ui.input(f, "API token secret", "", true);
    AlertDialog dialog =
        new AlertDialog.Builder(a)
            .setTitle("Existing API key")
            .setView(Ui.wrapped(f))
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
                        if (id.getText().toString().trim().isEmpty()
                            || secret.getText().toString().trim().isEmpty())
                          throw new Exception("Auth ID and secret are required");
                        JSONObject cfg =
                            new JSONObject()
                                .put("server", server.getText().toString().trim())
                                .put("host_id", host.getText().toString().trim())
                                .put("fingerprint", pin.getText().toString().trim())
                                .put("auth_id", id.getText().toString().trim())
                                .put("secret", secret.getText().toString().trim());
                        Mobilecore.newClient(cfg.toString());
                        KeyProfiles.addConnection(a, name.getText().toString(), cfg.toString());
                        secret.setText("");
                        dialog.dismiss();
                        changed.run();
                      } catch (Exception e) {
                        message(e.getMessage());
                      }
                    }));
    dialog.show();
  }

  void storage() {
    storage("");
  }

  void storage(String profileID) {
    if (!idle()) return;
    try {
      String raw = Vault.load(a);
      if (raw.isEmpty()) {
        message("Connect to PBS-X first");
        return;
      }
      JSONObject entry = KeyProfiles.connection(a, profileID);
      JSONObject cfg = entry.getJSONObject("config");
      LinearLayout f = Ui.form(a);
      Ui.hint(
          f,
          "Optional credentials for wider backup/restore access. The automatic device identity is"
              + " retained. Clear both fields to return to the automatic key.");
      EditText id = Ui.input(f, "Storage auth ID", cfg.optString("storage_auth_id"), false),
          secret = Ui.input(f, "Storage secret · enter to change", "", true);
      AlertDialog dialog =
          new AlertDialog.Builder(a)
              .setTitle("Storage API key")
              .setView(Ui.wrapped(f))
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
                          String nextId = id.getText().toString().trim(),
                              nextSecret = secret.getText().toString().trim();
                          if (!nextId.isEmpty()
                              && nextSecret.isEmpty()
                              && nextId.equals(cfg.optString("storage_auth_id")))
                            nextSecret = cfg.optString("storage_secret");
                          cfg.put("storage_auth_id", nextId).put("storage_secret", nextSecret);
                          Mobilecore.newClient(cfg.toString());
                          KeyProfiles.updateConnection(a, entry.getString("id"), cfg);
                          secret.setText("");
                          dialog.dismiss();
                          changed.run();
                        } catch (Exception e) {
                          message(e.getMessage());
                        }
                      }));
      dialog.show();
    } catch (Exception e) {
      message(e.getMessage());
    }
  }
}
