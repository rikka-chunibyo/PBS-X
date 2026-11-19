package com.pbsx.android;

import android.content.Context;
import org.json.JSONObject;

/** One durable next occurrence per job, coalescing missed times rather than queueing them. */
final class ScheduleState {
  static void validate(String raw) throws Exception {
    JSONObject all = new JSONObject(raw);
    if (all.length() > 100) throw new Exception("Too many saved schedule occurrences");
    java.util.Iterator<String> keys = all.keys();
    while (keys.hasNext()) {
      String id = keys.next();
      if (!id.matches("[A-Za-z0-9_-]{1,64}")) throw new Exception("Invalid saved schedule job ID");
      JSONObject value = all.getJSONObject(id);
      Object due = value.opt("due");
      if (value.length() != 2
          || !(value.opt("signature") instanceof String)
          || value.getString("signature").isEmpty()
          || value.getString("signature").length() > 256
          || (!(due instanceof Integer) && !(due instanceof Long))
          || value.getLong("due") <= 0) throw new Exception("Invalid saved schedule occurrence");
    }
  }

  static synchronized boolean isDue(Context c, String id, long expected) throws Exception {
    JSONObject all =
        new JSONObject(c.getSharedPreferences("jobs-v1", 0).getString("schedule_state", "{}"));
    JSONObject state = all.optJSONObject(id);
    return state != null && expected > 0 && state.optLong("due") == expected;
  }

  static synchronized long due(Context c, BackupJob job, boolean advance) throws Exception {
    android.content.SharedPreferences prefs = c.getSharedPreferences("jobs-v1", 0);
    JSONObject all = new JSONObject(prefs.getString("schedule_state", "{}"));
    String signature =
        job.calendar
            ? "calendar/"
                + job.scheduleDays
                + "/"
                + CalendarSchedule.times(job.scheduleTimes)
                + "/"
                + (job.scheduleZone.isEmpty()
                    ? java.time.ZoneId.systemDefault().getId()
                    : job.scheduleZone)
            : "interval/" + job.hours;
    JSONObject old = all.optJSONObject(job.id);
    if (!advance
        && old != null
        && signature.equals(old.optString("signature"))
        && old.optLong("due") > 0) return old.getLong("due");
    long now = System.currentTimeMillis();
    long due =
        !advance && old != null && old.optLong("due") > 0 && old.getLong("due") <= now
            ? old.getLong("due")
            : job.nextDue(now);
    all.put(job.id, new JSONObject().put("signature", signature).put("due", due));
    if (!prefs.edit().putString("schedule_state", all.toString()).commit())
      throw new Exception("Could not save the next schedule time");
    return due;
  }

  static synchronized void remove(Context c, String id) throws Exception {
    android.content.SharedPreferences prefs = c.getSharedPreferences("jobs-v1", 0);
    JSONObject all = new JSONObject(prefs.getString("schedule_state", "{}"));
    if (!all.has(id)) return;
    all.remove(id);
    if (!prefs.edit().putString("schedule_state", all.toString()).commit())
      throw new Exception("Could not remove the schedule time");
  }

  static synchronized void refreshClock(Context c, BackupJob job) throws Exception {
    if (!job.calendar) return;
    android.content.SharedPreferences prefs = c.getSharedPreferences("jobs-v1", 0);
    JSONObject all = new JSONObject(prefs.getString("schedule_state", "{}"));
    JSONObject old = all.optJSONObject(job.id);
    // Preserve an already-due occurrence that is waiting on constraints.
    if (old != null && old.optLong("due") <= System.currentTimeMillis()) return;
    remove(c, job.id);
  }
}
