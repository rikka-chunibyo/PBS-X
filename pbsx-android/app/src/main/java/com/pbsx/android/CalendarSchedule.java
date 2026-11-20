package com.pbsx.android;

import java.time.Instant;
import java.time.LocalDate;
import java.time.LocalTime;
import java.time.ZoneId;
import java.time.ZonedDateTime;
import java.util.TreeSet;

/** Calendar calculation only; OS constraints may defer execution beyond the due time. */
final class CalendarSchedule {
  static TreeSet<LocalTime> times(String text) {
    TreeSet<LocalTime> out = new TreeSet<>();
    for (String value : text.split(",", -1)) {
      String time = value.trim();
      if (!time.matches("(?:[01][0-9]|2[0-3]):[0-5][0-9]"))
        throw new IllegalArgumentException("Use times such as 02:00, 18:30");
      out.add(LocalTime.parse(time));
    }
    if (out.isEmpty() || out.size() > 8)
      throw new IllegalArgumentException("Choose between one and eight daily times");
    return out;
  }

  static long next(long after, String zone, int days, String times) {
    if (days <= 0 || days > 127) throw new IllegalArgumentException("Choose at least one weekday");
    ZoneId id = zone.isEmpty() ? ZoneId.systemDefault() : ZoneId.of(zone);
    TreeSet<LocalTime> candidates = times(times);
    ZonedDateTime start = Instant.ofEpochMilli(after).atZone(id);
    for (int offset = 0; offset <= 7; offset++) {
      LocalDate date = start.toLocalDate().plusDays(offset);
      if ((days & (1 << (date.getDayOfWeek().getValue() - 1))) == 0) continue;
      long earliest = Long.MAX_VALUE;
      for (LocalTime time : candidates) {
        // atZone shifts nonexistent spring-forward times to valid local time;
        // overlap times use the earlier offset and run only once that day.
        long due = date.atTime(time).atZone(id).toInstant().toEpochMilli();
        // Gap adjustment can reorder wall times (02:30 -> 03:30, while
        // 03:00 stays 03:00). Compare instants before choosing the occurrence.
        if (due > after) earliest = Math.min(earliest, due);
      }
      if (earliest != Long.MAX_VALUE) return earliest;
    }
    throw new IllegalArgumentException("No next schedule occurrence");
  }
}
