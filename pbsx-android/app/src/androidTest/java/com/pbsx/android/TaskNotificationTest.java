package com.pbsx.android;

import static org.junit.Assert.*;

import android.Manifest;
import android.app.Instrumentation;
import android.app.NotificationManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Build;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class TaskNotificationTest {
  private BackupService service() throws Exception {
    java.lang.reflect.Field field = BackupService.class.getDeclaredField("owner");
    field.setAccessible(true);
    return (BackupService) field.get(null);
  }

  private Operations.Cancellation token(BackupService service) throws Exception {
    java.lang.reflect.Field field = BackupService.class.getDeclaredField("cancellation");
    field.setAccessible(true);
    return (Operations.Cancellation) field.get(service);
  }

  private BackupService startHeld(Context c, BackupJob job) throws Exception {
    c.startForegroundService(
        new Intent(c, BackupService.class).putExtra("job_config", job.json().toString()));
    InstrumentationRegistry.getInstrumentation().waitForIdleSync();
    BackupService service = service();
    assertNotNull(service);
    long end = System.currentTimeMillis() + 5000;
    while (!Operations.isActive(token(service)) && System.currentTimeMillis() < end)
      Thread.sleep(20);
    assertTrue("Transfer did not acquire its operation", Operations.isActive(token(service)));
    return service;
  }

  private void awaitStopped() throws Exception {
    long end = System.currentTimeMillis() + 10000;
    while ((Operations.running() || BackupService.busy()) && System.currentTimeMillis() < end)
      Thread.sleep(20);
    assertFalse("Transfer did not stop", Operations.running());
    assertFalse("Service did not stop", BackupService.busy());
    InstrumentationRegistry.getInstrumentation().waitForIdleSync();
  }

  private void prepare(Instrumentation instrumentation, Context c) {
    if (Build.VERSION.SDK_INT >= 33
        && c.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
            != PackageManager.PERMISSION_GRANTED)
      instrumentation
          .getUiAutomation()
          .grantRuntimePermission(c.getPackageName(), Manifest.permission.POST_NOTIFICATIONS);
    instrumentation
        .getUiAutomation()
        .adoptShellPermissionIdentity(
            "android.permission.START_FOREGROUND_SERVICES_FROM_BACKGROUND");
  }

  private android.app.PendingIntent cancelAction(Context c) throws Exception {
    long end = System.currentTimeMillis() + 5000;
    do {
      for (android.service.notification.StatusBarNotification n :
          c.getSystemService(NotificationManager.class).getActiveNotifications())
        if (n.getId() == 71 && n.getNotification().actions != null)
          for (android.app.Notification.Action action : n.getNotification().actions)
            if ("Cancel".contentEquals(action.title)) return action.actionIntent;
      Thread.sleep(20);
    } while (System.currentTimeMillis() < end);
    throw new AssertionError("No cancel action was posted");
  }

  @Test
  public void timedOutServiceCannotRepostProgressFromALateCallback() throws Exception {
    Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    Context c = instrumentation.getTargetContext();
    prepare(instrumentation, c);
    try (TaskFixtureState fixture = new TaskFixtureState(c)) {
      JSONObject task = new PendingBackupsTest().run(c);
      PendingBackups.put(c, task);
      BackupJob job = BackupJob.from(task.getJSONObject("job"));
      try {
        synchronized (Vault.class) {
          BackupService service = startHeld(c, job);
          cancelAction(c);
          java.lang.reflect.Field field =
              BackupService.class.getDeclaredField("refreshNotification");
          field.setAccessible(true);
          Runnable late = (Runnable) field.get(service);
          instrumentation.runOnMainSync(
              () -> {
                service.onTimeout(
                    1, android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC);
                late
                    .run(); // Already queued before the timeout; must not recreate the
                            // notification.
              });
          instrumentation.waitForIdleSync();
          Thread.sleep(100);
          for (android.service.notification.StatusBarNotification n :
              c.getSystemService(NotificationManager.class).getActiveNotifications())
            assertNotEquals("Stopped service recreated its progress notification", 71, n.getId());
        }
        awaitStopped();
        assertEquals("paused", PendingBackups.get(c, job.id).getString("state"));
        assertEquals(task.getString("history"), PendingBackups.get(c, job.id).getString("history"));
      } finally {
        c.stopService(new Intent(c, BackupService.class));
        Operations.pause("Fixture cleanup", false);
        awaitStopped();
      }
    } finally {
      instrumentation.getUiAutomation().dropShellPermissionIdentity();
    }
  }

  @Test
  public void previousNotificationCannotCancelTheNextResumedTask() throws Exception {
    Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    Context c = instrumentation.getTargetContext();
    prepare(instrumentation, c);
    try (TaskFixtureState fixture = new TaskFixtureState(c)) {
      JSONObject task = new PendingBackupsTest().run(c);
      PendingBackups.put(c, task);
      BackupJob job = BackupJob.from(task.getJSONObject("job"));
      try {
        android.app.PendingIntent old;
        synchronized (Vault.class) {
          BackupService first = startHeld(c, job);
          old = cancelAction(c);
          c.startService(first.controlIntent("pause"));
          instrumentation.waitForIdleSync();
        }
        awaitStopped();
        synchronized (Vault.class) {
          BackupService second = startHeld(c, job);
          assertNotEquals(
              "Notification controls were reused across operations", old, cancelAction(c));
          old.send();
          instrumentation.waitForIdleSync();
          assertFalse("Old notification cancelled the resumed task", token(second).cancelled());
          assertFalse(token(second).paused());
          c.startService(second.controlIntent("pause"));
          instrumentation.waitForIdleSync();
        }
        awaitStopped();
        assertEquals("paused", PendingBackups.get(c, job.id).getString("state"));
        assertEquals(task.getString("history"), PendingBackups.get(c, job.id).getString("history"));
      } finally {
        c.stopService(new Intent(c, BackupService.class));
        Operations.pause("Fixture cleanup", false);
        awaitStopped();
      }
    } finally {
      instrumentation.getUiAutomation().dropShellPermissionIdentity();
    }
  }

  @Test
  public void pausingBeforeSourcePreparationRemovesTheForegroundNotification() throws Exception {
    Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    Context c = instrumentation.getTargetContext();
    boolean granted =
        Build.VERSION.SDK_INT < 33
            || c.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                == PackageManager.PERMISSION_GRANTED;
    if (!granted)
      instrumentation
          .getUiAutomation()
          .grantRuntimePermission(c.getPackageName(), Manifest.permission.POST_NOTIFICATIONS);
    try (TaskFixtureState state = new TaskFixtureState(c)) {
      JSONObject task = new PendingBackupsTest().run(c);
      PendingBackups.put(c, task);
      BackupJob job = BackupJob.from(task.getJSONObject("job"));
      // Hold preparation until both service commands have run on the main
      // thread. This exercises Pause even before the Go client is attached.
      instrumentation
          .getUiAutomation()
          .adoptShellPermissionIdentity(
              "android.permission.START_FOREGROUND_SERVICES_FROM_BACKGROUND");
      try {
        synchronized (Vault.class) {
          c.startForegroundService(
              new Intent(c, BackupService.class).putExtra("job_config", job.json().toString()));
          instrumentation.waitForIdleSync();
          c.startService(service().controlIntent("pause"));
          instrumentation.waitForIdleSync();
          long started = System.currentTimeMillis() + 5000;
          while (!Operations.running() && System.currentTimeMillis() < started) Thread.sleep(20);
          assertTrue("Transfer worker did not start", Operations.running());
          boolean posted = false;
          for (android.service.notification.StatusBarNotification n :
              c.getSystemService(NotificationManager.class).getActiveNotifications())
            if (n.getId() == 71) posted = true;
          assertTrue("Foreground notification was never posted", posted);
        }
      } finally {
        instrumentation.getUiAutomation().dropShellPermissionIdentity();
      }
      long end = System.currentTimeMillis() + 10000;
      boolean paused = false, notification = true;
      do {
        JSONObject pending = PendingBackups.get(c, job.id);
        paused =
            pending != null
                && "paused".equals(pending.optString("state"))
                && !Operations.running()
                && !BackupService.busy();
        notification = false;
        for (android.service.notification.StatusBarNotification n :
            c.getSystemService(NotificationManager.class).getActiveNotifications())
          if (n.getId() == 71) notification = true;
        if (!paused || notification) Thread.sleep(50);
      } while ((!paused || notification) && System.currentTimeMillis() < end);
      assertTrue("Backup did not retain its paused task", paused);
      assertFalse("Paused backup left a completion/progress notification", notification);
      assertEquals(task.getString("history"), PendingBackups.get(c, job.id).getString("history"));
      assertEquals(1, JobStore.history(c).length());
    } finally {
      c.stopService(new Intent(c, BackupService.class));
      // Revoking this permission can kill the instrumentation process. This
      // fixture runs on a disposable test emulator; leave the grant in place.
    }
  }
}
