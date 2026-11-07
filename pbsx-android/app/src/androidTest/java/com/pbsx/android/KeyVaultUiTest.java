package com.pbsx.android;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.view.View;
import android.view.ViewGroup;
import android.view.inspector.WindowInspector;
import android.widget.EditText;
import android.widget.ListView;
import android.widget.TextView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.Mobilecore;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
@android.annotation.TargetApi(29)
public class KeyVaultUiTest {
  private final Instrumentation i = InstrumentationRegistry.getInstrumentation();

  private View find(View root, String text) {
    if (root instanceof TextView && text.contentEquals(((TextView) root).getText())) return root;
    if (root instanceof ViewGroup)
      for (int n = 0; n < ((ViewGroup) root).getChildCount(); n++) {
        View v = find(((ViewGroup) root).getChildAt(n), text);
        if (v != null) return v;
      }
    return null;
  }

  private View global(String text) throws Exception {
    AtomicReference<View> result = new AtomicReference<>();
    long end = System.currentTimeMillis() + 5000;
    do {
      i.runOnMainSync(
          () -> {
            for (View root : WindowInspector.getGlobalWindowViews()) {
              View v = find(root, text);
              if (v != null && v.isShown()) result.set(v);
            }
          });
      if (result.get() != null) return result.get();
      Thread.sleep(25);
    } while (System.currentTimeMillis() < end);
    throw new AssertionError("Missing UI: " + text);
  }

  private void click(String text) throws Exception {
    View v = global(text);
    i.runOnMainSync(() -> assertTrue(v.performClick()));
    i.waitForIdleSync();
  }

  private void select(String text) throws Exception {
    View v = global(text);
    i.runOnMainSync(
        () -> {
          android.view.ViewParent parent = v.getParent();
          while (!(parent instanceof ListView)) parent = parent.getParent();
          ListView list = (ListView) parent;
          int n = list.getPositionForView(v);
          assertTrue(list.performItemClick(v, n, list.getAdapter().getItemId(n)));
        });
    i.waitForIdleSync();
  }

  private JSONObject config(String secret) throws Exception {
    return new JSONObject()
        .put("server", "https://fixture.invalid:8007")
        .put("host_id", "test-phone")
        .put("fingerprint", new String(new char[64]).replace('\0', '0'))
        .put("auth_id", "fixture@pbs!test")
        .put("secret", secret);
  }

  @Test
  public void selectsJobCredentialsAndShowsPasswordOnlyExportDialog() throws Exception {
    Context c = i.getTargetContext();
    Map<String, Map<String, ?>> old = new HashMap<>();
    for (String section : new String[] {"vault", "settings", "jobs-v1"}) {
      SharedPreferences p = c.getSharedPreferences(section, 0);
      old.put(section, p.getAll());
      p.edit().clear().commit();
    }
    Activity editor = null, keys = null;
    try {
      KeyProfiles.addConnection(c, "Phone access", config("phone-secret").toString());
      String second =
          KeyProfiles.addConnection(c, "Family access", config("family-secret").toString());
      String key = KeyProfiles.addKey(c, "Family encryption", Mobilecore.generateEncryptionKey());
      BackupJob job = BackupJob.create(c);
      job.sources.put(
          new JSONObject().put("label", "Photos").put("uri", "content://fixture/tree/photos"));
      JobStore.save(c, job);
      editor =
          i.startActivitySync(
              new Intent(c, JobEditorActivity.class)
                  .putExtra("job_id", job.id)
                  .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      click("Connection: Phone access");
      select("Family access");
      click("Encryption: Off");
      select("Family encryption");
      click("Save job");
      BackupJob saved = JobStore.get(c, job.id);
      assertEquals(second, saved.connectionID);
      assertEquals(key, saved.encryptionKeyID);
      keys =
          i.startActivitySync(
              new Intent(c, KeysActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      assertNotNull(global("PBS connections"));
      assertNotNull(global("Backup encryption keys"));
      assertNotNull(global("Import all settings"));
      click("Export all settings");
      assertNotNull(global("Plain JSON"));
      click("Use password");
      AtomicReference<EditText> password = new AtomicReference<>();
      i.runOnMainSync(
          () -> {
            for (View root : WindowInspector.getGlobalWindowViews()) findEdit(root, password);
            assertNotNull(password.get());
            assertFalse(password.get().isSaveEnabled());
            password.get().setText("export-password-fixture");
          });
      click("Cancel");
      assertFalse(Vault.data(c).toString().contains("export-password-fixture"));
      assertEquals(1, Vault.data(c).getJSONArray("keys").length());
    } finally {
      Activity a = editor, b = keys;
      i.runOnMainSync(
          () -> {
            if (a != null) a.finish();
            if (b != null) b.finish();
          });
      i.waitForIdleSync();
      for (String section : old.keySet()) {
        SharedPreferences.Editor e = c.getSharedPreferences(section, 0).edit().clear();
        for (Map.Entry<String, ?> entry : old.get(section).entrySet()) {
          Object v = entry.getValue();
          String k = entry.getKey();
          if (v instanceof String) e.putString(k, (String) v);
          else if (v instanceof Boolean) e.putBoolean(k, (Boolean) v);
          else if (v instanceof Integer) e.putInt(k, (Integer) v);
          else if (v instanceof Long) e.putLong(k, (Long) v);
          else if (v instanceof Float) e.putFloat(k, (Float) v);
          else if (v instanceof java.util.Set) {
            @SuppressWarnings("unchecked")
            java.util.Set<String> set = (java.util.Set<String>) v;
            e.putStringSet(k, set);
          } else throw new AssertionError("Unexpected preference type");
        }
        e.commit();
      }
      ScheduleService.configureAll(c);
    }
  }

  private void findEdit(View root, AtomicReference<EditText> out) {
    if (root instanceof EditText && root.isShown()) out.set((EditText) root);
    if (root instanceof ViewGroup)
      for (int n = 0; n < ((ViewGroup) root).getChildCount(); n++)
        findEdit(((ViewGroup) root).getChildAt(n), out);
  }
}
