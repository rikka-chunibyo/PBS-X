package com.pbsx.android;

import static org.junit.Assert.*;

import java.time.Instant;
import org.junit.Test;

public class CalendarScheduleTest {
  private long stamp(String text) {
    return Instant.parse(text).toEpochMilli();
  }

  @Test
  public void choosesSelectedWeekdaysAndTimesWithoutBacklog() {
    long monday = stamp("2026-11-16T09:00:00Z");
    assertEquals(
        stamp("2026-11-16T18:30:00Z"), CalendarSchedule.next(monday, "UTC", 1, "08:00, 18:30"));
    assertEquals(
        stamp("2026-11-23T08:00:00Z"),
        CalendarSchedule.next(stamp("2026-11-16T18:30:00Z"), "UTC", 1, "08:00,18:30"));
    assertEquals(
        stamp("2026-11-20T08:00:00Z"),
        CalendarSchedule.next(stamp("2026-11-19T23:59:00Z"), "UTC", 127, "08:00"));
  }

  @Test
  public void handlesDstGapAndOverlapOncePerLocalTime() {
    assertEquals(
        stamp("2026-03-08T07:30:00Z"),
        CalendarSchedule.next(stamp("2026-03-08T00:00:00Z"), "America/Toronto", 127, "02:30"));
    long first =
        CalendarSchedule.next(stamp("2026-11-01T00:00:00Z"), "America/Toronto", 127, "01:30");
    assertEquals(stamp("2026-11-01T05:30:00Z"), first);
    assertEquals(
        stamp("2026-11-02T06:30:00Z"),
        CalendarSchedule.next(first, "America/Toronto", 127, "01:30"));
  }

  @Test
  public void rejectsInvalidAndUnboundedSchedules() {
    for (String times :
        new String[] {
          "",
          "24:00",
          "12:60",
          "2:00",
          "02:00,",
          "00:00,01:00,02:00,03:00,04:00,05:00,06:00,07:00,08:00"
        }) {
      try {
        CalendarSchedule.next(0, "UTC", 127, times);
        fail(times);
      } catch (IllegalArgumentException expected) {
      }
    }
    try {
      CalendarSchedule.next(0, "UTC", 0, "02:00");
      fail("empty weekdays accepted");
    } catch (IllegalArgumentException expected) {
    }
  }
}
