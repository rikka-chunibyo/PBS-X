package com.pbsx.android;

import android.content.Context;
import android.content.SharedPreferences;
import java.util.ArrayList;
import java.util.List;
import org.json.JSONArray;
import org.json.JSONObject;

final class JobStore {
  private static SharedPreferences prefs(Context c) {
    return c.getSharedPreferences("jobs-v1", 0);
  }

  static synchronized List<BackupJob> all(Context c) throws Exception {
    SharedPreferences p = prefs(c);
    if (!p.contains("jobs")) {
      JSONArray jobs = new JSONArray();
      SharedPreferences old = Settings.prefs(c);
      if (old.contains("sources")
          || old.getBoolean("apks", false)
          || old.getBoolean("data", false)
          || old.getBoolean("obb", false)) {
        BackupJob j = new BackupJob();
        j.backupID = "";
        j.name = "Phone backup";
        j.schedulerId = 1000;
        j.sources = Settings.sources(c);
        j.store = Settings.store(c);
        j.namespace = Settings.namespace(c);
        j.hours = old.getInt("hours", 0);
        j.wifi = old.getBoolean("wifi", true);
        j.charging = old.getBoolean("charging", true);
        j.shizuku = old.getBoolean("shizuku", false);
        j.apks = old.getBoolean("apks", false);
        j.data = old.getBoolean("data", false);
        j.obb = old.getBoolean("obb", false);
        jobs.put(j.json());
      }
      if (!p.edit().putString("jobs", jobs.toString()).putInt("next_scheduler_id", 1001).commit())
        throw new Exception("Could not migrate backup jobs");
    }
    JSONArray array = new JSONArray(p.getString("jobs", "[]"));
    List<BackupJob> out = new ArrayList<>();
    for (int i = 0; i < array.length(); i++) out.add(BackupJob.from(array.getJSONObject(i)));
    return out;
  }

  static synchronized BackupJob get(Context c, String id) throws Exception {
    for (BackupJob j : all(c)) if (j.id.equals(id)) return j;
    throw new Exception("This job no longer exists");
  }

  static synchronized void save(Context c, BackupJob job) throws Exception {
    job.validate(false);
    List<BackupJob> jobs = all(c);
    boolean found = false;
    for (int i = 0; i < jobs.size(); i++)
      if (jobs.get(i).id.equals(job.id)) {
        job.schedulerId = jobs.get(i).schedulerId;
        jobs.set(i, job);
        found = true;
        break;
      }
    SharedPreferences p = prefs(c);
    if (!found) {
      if (jobs.size() >= 100) throw new Exception("At most 100 backup jobs are supported");
      job.schedulerId = p.getInt("next_scheduler_id", 1001);
      if (job.schedulerId < 1000 || job.schedulerId == Integer.MAX_VALUE)
        throw new Exception("Schedule identity limit reached");
      jobs.add(job);
    }
    JSONArray out = new JSONArray();
    for (BackupJob j : jobs) out.put(j.json());
    SharedPreferences.Editor e = p.edit().putString("jobs", out.toString());
    if (!found) e.putInt("next_scheduler_id", job.schedulerId + 1);
    if (!e.commit()) throw new Exception("Could not save job");
  }

  static synchronized void delete(Context c, String id) throws Exception {
    JSONArray out = new JSONArray();
    for (BackupJob j : all(c)) if (!j.id.equals(id)) out.put(j.json());
    if (!prefs(c).edit().putString("jobs", out.toString()).commit())
      throw new Exception("Could not remove job");
  }

  static synchronized String start(Context c, BackupJob job, String kind) throws Exception {
    JSONObject run =
        new JSONObject()
            .put("id", java.util.UUID.randomUUID().toString())
            .put("job_id", job.id)
            .put("job_name", job.name)
            .put("kind", kind)
            .put("target", job.target())
            .put("started", System.currentTimeMillis())
            .put("state", "running")
            .put("message", kind.equals("restore") ? "Preparing restore…" : "Preparing backup…");
    JSONArray old = new JSONArray(prefs(c).getString("history", "[]")),
        out = new JSONArray().put(run);
    for (int i = 0; i < old.length() && i < 99; i++) out.put(old.get(i));
    prefs(c).edit().putString("history", out.toString()).commit();
    return run.getString("id");
  }

  static synchronized void finish(Context c, String id, boolean success, String message)
      throws Exception {
    JSONArray out = new JSONArray(prefs(c).getString("history", "[]"));
    for (int i = 0; i < out.length(); i++) {
      JSONObject r = out.getJSONObject(i);
      if (id.equals(r.getString("id"))) {
        r.put("state", success ? "success" : "failed")
            .put("finished", System.currentTimeMillis())
            .put("message", message.length() > 4096 ? message.substring(0, 4096) : message);
        break;
      }
    }
    prefs(c).edit().putString("history", out.toString()).commit();
  }

  static synchronized void progress(Context c, String id, String message) throws Exception {
    JSONArray runs = history(c);
    for (int i = 0; i < runs.length(); i++) {
      JSONObject run = runs.getJSONObject(i);
      if (id.equals(run.optString("id")) && "running".equals(run.optString("state"))) {
        run.put("message", message.length() > 4096 ? message.substring(0, 4096) : message);
        prefs(c).edit().putString("history", runs.toString()).apply();
        return;
      }
    }
  }

  static synchronized JSONArray history(Context c) throws Exception {
    return new JSONArray(prefs(c).getString("history", "[]"));
  }

  static synchronized void recover(Context c) throws Exception {
    if (Operations.running()) return;
    JSONArray out = history(c);
    boolean changed = false;
    for (int i = 0; i < out.length(); i++) {
      JSONObject r = out.getJSONObject(i);
      if ("running".equals(r.optString("state"))) {
        r.put("state", "interrupted")
            .put("finished", System.currentTimeMillis())
            .put(
                "message",
                "App stopped before completion; no completed restore or backup was claimed");
        changed = true;
      }
    }
    if (changed) prefs(c).edit().putString("history", out.toString()).commit();
  }
}
