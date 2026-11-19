package com.pbsx.android;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.view.View;
import android.view.ViewGroup;
import android.widget.TextView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileOutputStream;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONObject;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Exercises visible navigation against the local HTTPS archive fixture. */
@RunWith(AndroidJUnit4.class)
public class NavigationTest {
  private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();

  private View find(View root, String label, boolean description) {
    if (description
        ? label.contentEquals(
            root.getContentDescription() == null ? "" : root.getContentDescription())
        : root instanceof TextView && label.contentEquals(((TextView) root).getText())) return root;
    if (root instanceof ViewGroup)
      for (int i = 0; i < ((ViewGroup) root).getChildCount(); i++) {
        View found = find(((ViewGroup) root).getChildAt(i), label, description);
        if (found != null) return found;
      }
    return null;
  }

  private View waitFor(Activity activity, String text, boolean description) throws Exception {
    long end = System.currentTimeMillis() + 10000;
    AtomicReference<View> found = new AtomicReference<>();
    do {
      instrumentation.runOnMainSync(
          () -> found.set(find(activity.findViewById(android.R.id.content), text, description)));
      if (found.get() != null) return found.get();
      Thread.sleep(50);
    } while (System.currentTimeMillis() < end);
    throw new AssertionError("UI did not show " + text);
  }

  private void click(Activity activity, String label) throws Exception {
    View v = waitFor(activity, label, false);
    instrumentation.runOnMainSync(
        () -> {
          boolean consumed = v.performClick();
          // CompoundButton toggles even when no ordinary click listener consumes it.
          assertTrue(
              "Click was not consumed: " + label,
              consumed || v instanceof android.widget.CompoundButton);
        });
  }

  private void back(Activity activity) throws Exception {
    // ATD has no focused rendered display for injected hardware Back events.
    // Exercise the app's Back button and the same navigateBack state machine.
    View button = waitFor(activity, "Back", true);
    instrumentation.runOnMainSync(() -> assertTrue(button.performClick()));
  }

  private void capture(Activity activity, String name) throws Exception {
    capture(activity.getWindow().getDecorView(), name);
  }

  private void capture(View decor, String name) throws Exception {
    if (!"true".equals(InstrumentationRegistry.getArguments().getString("pbsx_screenshots")))
      return;
    instrumentation.waitForIdleSync();
    Thread.sleep(250);
    // ATD disables SurfaceFlinger output. Draw the actual laid-out Android views
    // into a software canvas for review; this is not a hardware screenshot.
    AtomicReference<Bitmap> rendered = new AtomicReference<>();
    instrumentation.runOnMainSync(
        () -> {
          Bitmap bitmap =
              Bitmap.createBitmap(decor.getWidth(), decor.getHeight(), Bitmap.Config.ARGB_8888);
          decor.draw(new Canvas(bitmap));
          rendered.set(bitmap);
        });
    Bitmap bitmap = rendered.get();
    try (FileOutputStream output =
        new FileOutputStream(
            new File(instrumentation.getTargetContext().getCacheDir(), name + ".png"))) {
      assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output));
    } finally {
      bitmap.recycle();
    }
  }

  @Test
  public void jobsTabsEditorAndSnapshotSelectionNavigateCorrectly() throws Exception {
    Assume.assumeTrue(android.os.Build.VERSION.SDK_INT >= 29);
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(config != null && !config.isEmpty());
    Context c = instrumentation.getTargetContext();
    SharedPreferences jobs = c.getSharedPreferences("jobs-v1", 0);
    Map<String, ?> old = new HashMap<>(jobs.getAll());
    String oldVault = Vault.load(c);
    Activity main = null, restore = null, editor = null;
    try {
      jobs.edit().clear().commit();
      Vault.save(c, config);
      BackupJob job = new BackupJob();
      job.backupID = "android-test";
      job.name = "Camera test";
      job.sources.put(
          new JSONObject().put("label", "Photos").put("uri", "content://fixture/tree/photos"));
      JobStore.save(c, job);
      main =
          instrumentation.startActivitySync(
              new Intent(c, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      waitFor(main, "Camera test", false);
      capture(main, "jobs");
      click(main, "Activity");
      waitFor(main, "Recent backup and restore runs on this device.", false);
      click(main, "Settings");
      waitFor(main, "Server & credentials", false);
      click(main, "Jobs");
      Instrumentation.ActivityMonitor browser =
          instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      click(main, "Snapshots");
      restore = instrumentation.waitForMonitorWithTimeout(browser, 5000);
      instrumentation.removeMonitor(browser);
      assertNotNull(restore);
      click(restore, "Browse files");
      View photos = waitFor(restore, "Photos", false);
      instrumentation.runOnMainSync(() -> assertTrue(((View) photos.getParent()).performClick()));
      View choose = waitFor(restore, "Select a.txt", true);
      // CompoundButton toggles even when no ordinary OnClickListener consumes the click.
      instrumentation.runOnMainSync(choose::performClick);
      waitFor(restore, "Selection · 1", false);
      capture(restore, "restore-files");
      click(restore, "Restore selected");
      instrumentation.waitForIdleSync();
      AtomicReference<View> options = new AtomicReference<>();
      instrumentation.runOnMainSync(
          () -> {
            for (View window : android.view.inspector.WindowInspector.getGlobalWindowViews())
              if (find(window, "New folder (default)", false) != null) options.set(window);
          });
      assertNotNull("Restore options dialog was not shown", options.get());
      for (String label :
          new String[] {
            "New folder (default)",
            "Skip existing files",
            "Replace different files",
            "Rename conflicting files"
          }) {
        AtomicReference<View> option = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> option.set(find(options.get(), label, false)));
        assertNotNull("Missing restore mode " + label, option.get());
      }
      capture(options.get(), "restore-modes");
      instrumentation.runOnMainSync(
          () -> assertTrue(find(options.get(), "Cancel", false).performClick()));
      long closeDeadline = System.currentTimeMillis() + 5000;
      AtomicReference<Boolean> attached = new AtomicReference<>(true);
      do {
        instrumentation.runOnMainSync(() -> attached.set(options.get().isAttachedToWindow()));
        if (attached.get()) Thread.sleep(50);
      } while (attached.get() && System.currentTimeMillis() < closeDeadline);
      assertFalse("Restore options did not close", attached.get());
      back(restore);
      waitFor(restore, "Snapshot contents", false);
      waitFor(restore, "Selection · 1", false);
      back(restore);
      waitFor(restore, "Browse files", false);
      back(restore);
      restore = null;
      Instrumentation.ActivityMonitor edit =
          instrumentation.addMonitor(JobEditorActivity.class.getName(), null, false);
      click(main, "+ New job");
      editor = instrumentation.waitForMonitorWithTimeout(edit, 5000);
      instrumentation.removeMonitor(edit);
      assertNotNull(editor);
      waitFor(editor, "Schedule & connection", false);
      waitFor(editor, "Optional Shizuku sources", false);
    } finally {
      final Activity a = editor, b = restore, d = main;
      instrumentation.runOnMainSync(
          () -> {
            if (a != null && !a.isFinishing()) a.finish();
            if (b != null && !b.isFinishing()) b.finish();
            if (d != null && !d.isFinishing()) d.finish();
          });
      SharedPreferences.Editor reset = jobs.edit().clear();
      for (Map.Entry<String, ?> item : old.entrySet()) {
        Object value = item.getValue();
        if (value instanceof String) reset.putString(item.getKey(), (String) value);
        else if (value instanceof Integer) reset.putInt(item.getKey(), (Integer) value);
        else if (value instanceof Long) reset.putLong(item.getKey(), (Long) value);
        else if (value instanceof Boolean) reset.putBoolean(item.getKey(), (Boolean) value);
      }
      reset.commit();
      if (oldVault.isEmpty())
        c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, oldVault);
      ScheduleService.configureAll(c);
    }
  }

  private void search(Activity activity, String query, String absent) throws Exception {
    android.widget.EditText field =
        (android.widget.EditText) waitFor(activity, "Search backups", true);
    instrumentation.runOnMainSync(() -> field.setText(query));
    long deadline = System.currentTimeMillis() + 10000;
    java.util.concurrent.atomic.AtomicBoolean gone =
        new java.util.concurrent.atomic.AtomicBoolean();
    do {
      instrumentation.runOnMainSync(
          () -> gone.set(find(activity.findViewById(android.R.id.content), absent, false) == null));
      if (!gone.get()) Thread.sleep(50);
    } while (!gone.get() && System.currentTimeMillis() < deadline);
    assertTrue("Search did not filter " + absent, gone.get());
  }

  @Test
  public void jobEditorSelectsHistoryWithoutAdoptingDeviceIdentity() throws Exception {
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(config != null && !config.isEmpty());
    Context c = instrumentation.getTargetContext();
    String oldVault = Vault.load(c);
    Activity editor = null, picker = null, main = null, restore = null;
    BackupJob job = null;
    try {
      Vault.save(c, config);
      job = BackupJob.create(c);
      assertEquals("devices/android-test", job.namespace);
      job.name = "Picker test";
      job.sources.put(
          new JSONObject().put("label", "Photos").put("uri", "content://fixture/tree/photos"));
      JobStore.save(c, job);
      editor =
          instrumentation.startActivitySync(
              new Intent(c, JobEditorActivity.class)
                  .putExtra("job_id", job.id)
                  .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      android.widget.EditText name =
          (android.widget.EditText) waitFor(editor, "Picker test", false);
      instrumentation.runOnMainSync(() -> name.setText("Cosmetic label"));
      capture(editor, "job-identity");
      Instrumentation.ActivityMonitor monitor =
          instrumentation.addMonitor(SelectBackupActivity.class.getName(), null, false);
      click(editor, "Use existing backup");
      picker = instrumentation.waitForMonitorWithTimeout(monitor, 5000);
      instrumentation.removeMonitor(monitor);
      assertNotNull(picker);
      View label = waitFor(picker, "host/family-laptop", false);
      capture(picker, "backup-history-picker");
      Activity selection = picker;
      instrumentation.runOnMainSync(
          () -> {
            assertNull(find(selection.findViewById(android.R.id.content), "vm/100", false));
            assertNull(find(selection.findViewById(android.R.id.content), "ct/101", false));
            assertTrue(find((View) label.getParent(), "Use this backup", false).performClick());
          });
      picker = null;
      waitFor(editor, "family-laptop", false);
      click(editor, "Save job");
      editor = null;
      BackupJob saved = JobStore.get(c, job.id);
      assertEquals("Cosmetic label", saved.name);
      assertEquals("family-laptop", saved.backupID);
      assertEquals("", saved.namespace);
      assertEquals("android-test", new JSONObject(Vault.load(c)).getString("host_id"));
      main =
          instrumentation.startActivitySync(
              new Intent(c, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      View jobLabel = waitFor(main, "Cosmetic label", false);
      monitor = instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      instrumentation.runOnMainSync(
          () ->
              assertTrue(
                  find((View) jobLabel.getParent().getParent(), "Snapshots", false)
                      .performClick()));
      restore = instrumentation.waitForMonitorWithTimeout(monitor, 5000);
      instrumentation.removeMonitor(monitor);
      assertNotNull(restore);
      assertEquals("family-laptop", restore.getIntent().getStringExtra("source"));
      click(restore, "Browse files");
      waitFor(restore, "Snapshot contents", false);
    } finally {
      Activity a = restore, b = main, d = picker, e = editor;
      instrumentation.runOnMainSync(
          () -> {
            if (a != null) a.finish();
            if (b != null) b.finish();
            if (d != null) d.finish();
            if (e != null) e.finish();
          });
      if (job != null) JobStore.delete(c, job.id);
      if (oldVault.isEmpty())
        c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, oldVault);
      ScheduleService.configureAll(c);
    }
  }

  @Test
  public void newJobsStartWithNoShizukuSelectionsAndDisableDependentControls() throws Exception {
    BackupJob defaults = new BackupJob();
    assertFalse(defaults.shizuku);
    assertFalse(defaults.apks);
    assertFalse(defaults.data);
    assertFalse(defaults.obb);
    assertFalse(defaults.allApps);
    assertEquals(0, defaults.packages.length());
    assertEquals(0, defaults.exports.length());
    assertEquals(0, defaults.shizukuFolders.length());
    Activity editor =
        instrumentation.startActivitySync(
            new Intent(instrumentation.getTargetContext(), JobEditorActivity.class)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
    try {
      final Activity screen = editor;
      instrumentation.runOnMainSync(
          () -> {
            View root = screen.findViewById(android.R.id.content);
            java.util.List<String> labels = new java.util.ArrayList<>();
            labels.add("Installed application APKs");
            labels.add("Android/data");
            labels.add("Android/obb");
            labels.add("All APKs");
            labels.add("All Android/data folders");
            labels.add("All Android/obb folders");
            java.util.Collections.addAll(labels, DeviceExport.LABELS);
            for (String label : labels) {
              android.widget.CheckBox box = (android.widget.CheckBox) find(root, label, false);
              assertNotNull(label, box);
              assertFalse(label, box.isChecked());
              assertFalse(label, box.isEnabled());
              assertEquals(label, 0.4f, box.getAlpha(), 0.01f);
            }
            assertFalse(find(root, "Choose apps · APKs", false).isEnabled());
            assertFalse(find(root, "Choose apps · Android/data", false).isEnabled());
            assertFalse(find(root, "Choose apps · Android/obb", false).isEnabled());
            assertFalse(find(root, "+ Add folder via Shizuku", false).isEnabled());
          });
      click(editor, "Enable Shizuku for this job");
      instrumentation.runOnMainSync(
          () -> {
            View root = screen.findViewById(android.R.id.content);
            for (String label :
                new String[] {
                  "Installed application APKs",
                  "Android/data",
                  "Android/obb",
                  "+ Add folder via Shizuku"
                }) {
              View control = find(root, label, false);
              assertTrue(label, control.isEnabled());
              assertEquals(1f, control.getAlpha(), 0.01f);
            }
            for (String label : DeviceExport.LABELS)
              assertTrue(label, find(root, label, false).isEnabled());
          });
      click(editor, "Installed application APKs");
      instrumentation.runOnMainSync(
          () -> {
            View root = screen.findViewById(android.R.id.content);
            assertTrue(find(root, "Choose apps · APKs", false).isEnabled());
            assertFalse(find(root, "Choose apps · Android/data", false).isEnabled());
          });
      click(editor, "SMS messages");
      click(editor, "Enable Shizuku for this job");
      instrumentation.runOnMainSync(
          () -> {
            android.widget.CheckBox box =
                (android.widget.CheckBox)
                    find(screen.findViewById(android.R.id.content), "SMS messages", false);
            assertFalse(box.isEnabled());
            assertTrue("Disabling Shizuku should preserve choices", box.isChecked());
          });
      click(editor, "Enable Shizuku for this job");
      instrumentation.runOnMainSync(
          () -> {
            android.widget.CheckBox box =
                (android.widget.CheckBox)
                    find(screen.findViewById(android.R.id.content), "SMS messages", false);
            assertTrue(box.isEnabled());
            assertTrue(box.isChecked());
          });
    } finally {
      Activity screen = editor;
      instrumentation.runOnMainSync(screen::finish);
    }
  }

  @Test
  public void editorSavesAppSelectionAndIndependentDeviceExports() throws Exception {
    Assume.assumeTrue(rikka.shizuku.Shizuku.pingBinder());
    Assume.assumeTrue(
        rikka.shizuku.Shizuku.checkSelfPermission()
            == android.content.pm.PackageManager.PERMISSION_GRANTED);
    Context c = instrumentation.getTargetContext();
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(config != null && !config.isEmpty());
    String oldVault = Vault.load(c);
    Activity editor = null;
    BackupJob job = null;
    try {
      Vault.save(c, config);
      job = BackupJob.create(c);
      job.name = "Source choices";
      job.shizuku = true;
      job.apks = true;
      job.allApps = false;
      job.packages.put("com.pbsx.android");
      job.exports.put("sms");
      JobStore.save(c, job);
      editor =
          instrumentation.startActivitySync(
              new Intent(c, JobEditorActivity.class)
                  .putExtra("job_id", job.id)
                  .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      click(editor, "Contacts");
      click(editor, "Call history");
      click(editor, "Choose apps · APKs");
      AtomicReference<View> options = new AtomicReference<>();
      long deadline = System.currentTimeMillis() + 15000;
      do {
        instrumentation.runOnMainSync(
            () -> {
              for (View window : android.view.inspector.WindowInspector.getGlobalWindowViews())
                if (find(window, "Choose APKs", false) != null) options.set(window);
            });
        if (options.get() == null) Thread.sleep(50);
      } while (options.get() == null && System.currentTimeMillis() < deadline);
      assertNotNull("App picker did not appear", options.get());
      instrumentation.runOnMainSync(
          () -> assertTrue(find(options.get(), "Use selected", false).performClick()));
      deadline = System.currentTimeMillis() + 5000;
      java.util.concurrent.atomic.AtomicBoolean attached =
          new java.util.concurrent.atomic.AtomicBoolean(true);
      do {
        instrumentation.runOnMainSync(() -> attached.set(options.get().isAttachedToWindow()));
        if (attached.get()) Thread.sleep(50);
      } while (attached.get() && System.currentTimeMillis() < deadline);
      assertFalse("App picker did not close", attached.get());
      waitFor(editor, "Selected APKs: 1", false);
      View title = waitFor(editor, "Optional Shizuku sources", false);
      instrumentation.runOnMainSync(
          () -> {
            android.view.ViewParent parent = title.getParent();
            while (parent != null && !(parent instanceof android.widget.ScrollView))
              parent = parent.getParent();
            if (parent != null)
              ((android.widget.ScrollView) parent).scrollTo(0, ((View) title.getParent()).getTop());
          });
      capture(editor, "shizuku-source-options");
      click(editor, "Save job");
      editor = null;
      BackupJob saved = JobStore.get(c, job.id);
      assertFalse(saved.allApps);
      assertTrue(saved.apks);
      assertEquals("[\"com.pbsx.android\"]", saved.packages.toString());
      assertEquals("[\"contacts\",\"sms\",\"calls\"]", saved.exports.toString());
    } finally {
      Activity a = editor;
      instrumentation.runOnMainSync(
          () -> {
            if (a != null) a.finish();
          });
      if (job != null) JobStore.delete(c, job.id);
      if (oldVault.isEmpty())
        c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, oldVault);
      ScheduleService.configureAll(c);
    }
  }

  @Test
  public void restoreCatalogueFindsOtherHostsNamespacesAndBackupTypes() throws Exception {
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(config != null && !config.isEmpty());
    Context c = instrumentation.getTargetContext();
    String oldVault = Vault.load(c);
    Activity main = null, restore = null;
    try {
      Vault.save(c, config);
      main =
          instrumentation.startActivitySync(
              new Intent(c, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
      click(main, "Restore");
      waitFor(main, "host/family-laptop", false);
      waitFor(main, "host/grandmas-phone", false);
      waitFor(main, "vm/100", false);
      waitFor(main, "ct/101", false);
      capture(main, "restore-catalogue");
      search(main, "phones/kids", "host/family-laptop");
      waitFor(main, "host/grandmas-phone", false);
      search(main, "ct/101", "host/grandmas-phone");
      View label = waitFor(main, "ct/101", false);
      Instrumentation.ActivityMonitor browser =
          instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      instrumentation.runOnMainSync(
          () -> assertTrue(find((View) label.getParent(), "Snapshots", false).performClick()));
      restore = instrumentation.waitForMonitorWithTimeout(browser, 5000);
      instrumentation.removeMonitor(browser);
      assertNotNull(restore);
      assertEquals("ct", restore.getIntent().getStringExtra("source_type"));
      assertEquals("101", restore.getIntent().getStringExtra("source"));
      assertEquals("family", restore.getIntent().getStringExtra("store"));
      click(restore, "Browse files");
      waitFor(restore, "Snapshot contents", false);
      back(restore);
      waitFor(restore, "Browse files", false);
      back(restore);
      restore = null;
      waitFor(main, "ct/101", false);
      search(main, "vm/100", "ct/101");
      View vm = waitFor(main, "vm/100", false);
      browser = instrumentation.addMonitor(RestoreActivity.class.getName(), null, false);
      instrumentation.runOnMainSync(
          () -> assertTrue(find((View) vm.getParent(), "Snapshots", false).performClick()));
      restore = instrumentation.waitForMonitorWithTimeout(browser, 5000);
      instrumentation.removeMonitor(browser);
      assertNotNull(restore);
      View browse = waitFor(restore, "Browse files", false);
      instrumentation.runOnMainSync(
          () ->
              assertFalse("Image-only snapshots must not offer file restore", browse.isEnabled()));
      capture(restore, "restore-vm-snapshots");
    } finally {
      Activity a = restore, b = main;
      instrumentation.runOnMainSync(
          () -> {
            if (a != null) a.finish();
            if (b != null) b.finish();
          });
      if (oldVault.isEmpty())
        c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, oldVault);
    }
  }
}
