package com.pbsx.android;

import static org.junit.Assert.*;

import org.junit.Test;

public class PathsTest {
  @Test
  public void acceptsPortableNames() {
    assertEquals(0, Paths.parts(".").length);
    assertArrayEquals(new String[] {"Photos", "猫 photo.jpg"}, Paths.parts("Photos/猫 photo.jpg"));
  }

  @Test
  public void rejectsTraversalAndAmbiguity() {
    for (String path :
        new String[] {"", "/x", "../x", "x/..", "x//y", "x/./y", "x\\y", "x\0y", "x/"}) {
      try {
        Paths.parts(path);
        fail(path);
      } catch (IllegalArgumentException expected) {
      }
    }
  }
}
