package com.pbsx.android;

import android.app.Activity;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.os.Build;
import android.text.InputType;
import android.view.View;
import android.view.WindowInsetsController;
import android.view.WindowManager;
import android.widget.*;

final class Ui {
  static final int TEXT = Color.rgb(237, 242, 252),
      HEADER = Color.rgb(20, 32, 51),
      SURFACE = Color.rgb(25, 37, 56),
      SECONDARY = Color.rgb(38, 52, 75),
      PRIMARY = Color.rgb(57, 87, 190),
      BLUE = Color.rgb(156, 182, 255),
      MUTED = Color.rgb(167, 180, 202),
      BG = Color.rgb(14, 22, 36),
      LINE = Color.rgb(57, 72, 96);

  static int dp(Context c, int n) {
    return (int) (n * c.getResources().getDisplayMetrics().density + .5f);
  }

  static GradientDrawable shape(int color, int radius) {
    GradientDrawable d = new GradientDrawable();
    d.setColor(color);
    d.setCornerRadius(radius);
    return d;
  }

  static LinearLayout column(Context c) {
    LinearLayout p = new LinearLayout(c);
    p.setOrientation(LinearLayout.VERTICAL);
    return p;
  }

  static LinearLayout row(Context c) {
    LinearLayout p = new LinearLayout(c);
    p.setOrientation(LinearLayout.HORIZONTAL);
    p.setGravity(android.view.Gravity.CENTER_VERTICAL);
    return p;
  }

  static TextView text(LinearLayout p, String value, int size, int color) {
    Context c = p.getContext();
    TextView t = new TextView(c);
    t.setText(value);
    t.setTextSize(size);
    t.setTextColor(color);
    t.setPadding(0, dp(c, 4), 0, dp(c, 6));
    p.addView(t);
    return t;
  }

  static TextView title(LinearLayout p, String value) {
    TextView t = text(p, value, 25, TEXT);
    t.setTypeface(null, Typeface.BOLD);
    return t;
  }

  static TextView hint(LinearLayout p, String value) {
    return text(p, value, 14, MUTED);
  }

  static void gap(LinearLayout p, int height) {
    View v = new View(p.getContext());
    p.addView(v, new LinearLayout.LayoutParams(1, dp(p.getContext(), height)));
  }

  static LinearLayout card(LinearLayout parent) {
    Context c = parent.getContext();
    LinearLayout p = column(c);
    p.setPadding(dp(c, 18), dp(c, 14), dp(c, 18), dp(c, 14));
    p.setBackground(shape(SURFACE, dp(c, 16)));
    p.setElevation(dp(c, 1));
    LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, -2);
    lp.setMargins(0, dp(c, 6), 0, dp(c, 10));
    parent.addView(p, lp);
    return p;
  }

  static Button button(LinearLayout p, String text, boolean primary, View.OnClickListener action) {
    Context c = p.getContext();
    Button b = new Button(c);
    b.setText(text);
    b.setTextSize(14);
    b.setAllCaps(false);
    b.setTextColor(primary ? Color.WHITE : BLUE);
    b.setBackgroundTintList(
        android.content.res.ColorStateList.valueOf(primary ? PRIMARY : SECONDARY));
    b.setMinHeight(dp(c, 46));
    b.setOnClickListener(action);
    LinearLayout.LayoutParams lp =
        new LinearLayout.LayoutParams(
            p.getOrientation() == LinearLayout.HORIZONTAL ? 0 : -1,
            -2,
            p.getOrientation() == LinearLayout.HORIZONTAL ? 1 : 0);
    lp.setMargins(dp(c, 2), dp(c, 4), dp(c, 2), dp(c, 2));
    p.addView(b, lp);
    return b;
  }

  static EditText input(LinearLayout p, String label, String value, boolean secret) {
    Context c = p.getContext();
    hint(p, label);
    EditText e = new EditText(c);
    e.setText(value);
    e.setSingleLine(true);
    e.setTextSize(16);
    e.setTextColor(TEXT);
    e.setInputType(
        InputType.TYPE_CLASS_TEXT
            | (secret
                ? InputType.TYPE_TEXT_VARIATION_PASSWORD
                : InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS));
    if (secret) e.setSaveEnabled(false);
    e.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
    p.addView(e, new LinearLayout.LayoutParams(-1, dp(c, 48)));
    return e;
  }

  static CheckBox check(LinearLayout p, String label, boolean checked) {
    CheckBox b = new CheckBox(p.getContext());
    b.setText(label);
    b.setTextColor(TEXT);
    b.setChecked(checked);
    p.addView(b);
    return b;
  }

  static void systemBars(Activity a) {
    a.getWindow().setStatusBarColor(HEADER);
    a.getWindow().setNavigationBarColor(BG);
    a.getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
    // Materialize the decor before PhoneWindow accesses its insets controller.
    View decor = a.getWindow().getDecorView();
    if (Build.VERSION.SDK_INT >= 30) {
      a.getWindow().setDecorFitsSystemWindows(false);
      WindowInsetsController controller = a.getWindow().getInsetsController();
      if (controller != null)
        controller.setSystemBarsAppearance(
            0,
            WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS
                | WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS);
    } else {
      decor.setSystemUiVisibility(0);
    }
  }

  static LinearLayout screen(Activity a, String title) {
    systemBars(a);
    LinearLayout root = column(a);
    root.setBackgroundColor(BG);
    LinearLayout bar = row(a);
    bar.setPadding(dp(a, 12), dp(a, 8), dp(a, 16), dp(a, 8));
    bar.setBackgroundColor(HEADER);
    Button back = new Button(a);
    back.setText("‹");
    back.setTextSize(28);
    back.setTextColor(Color.WHITE);
    back.setBackgroundColor(HEADER);
    back.setContentDescription("Back");
    back.setOnClickListener(v -> a.finish());
    bar.addView(back, new LinearLayout.LayoutParams(dp(a, 46), dp(a, 48)));
    TextView text = new TextView(a);
    text.setText(title);
    text.setTextSize(20);
    text.setTextColor(Color.WHITE);
    text.setTypeface(null, Typeface.BOLD);
    bar.addView(text);
    root.addView(bar);
    if (Build.VERSION.SDK_INT >= 30)
      root.setOnApplyWindowInsetsListener(
          (v, insets) -> {
            android.graphics.Insets bars =
                insets.getInsets(android.view.WindowInsets.Type.systemBars());
            int keyboard = insets.getInsets(android.view.WindowInsets.Type.ime()).bottom;
            bar.setPadding(dp(a, 12), dp(a, 8) + bars.top, dp(a, 16), dp(a, 8));
            v.setPadding(bars.left, 0, bars.right, Math.max(bars.bottom, keyboard));
            return insets;
          });
    a.setContentView(root);
    return root;
  }

  static LinearLayout scroll(LinearLayout root) {
    Context c = root.getContext();
    ScrollView scroll = new ScrollView(c);
    scroll.setFillViewport(true);
    LinearLayout p = column(c);
    p.setPadding(dp(c, 18), dp(c, 12), dp(c, 18), dp(c, 20));
    scroll.addView(p);
    root.addView(scroll, new LinearLayout.LayoutParams(-1, 0, 1));
    return p;
  }

  static LinearLayout form(Context c) {
    LinearLayout f = column(c);
    f.setPadding(dp(c, 22), dp(c, 12), dp(c, 22), dp(c, 10));
    return f;
  }

  static ScrollView wrapped(LinearLayout p) {
    ScrollView s = new ScrollView(p.getContext());
    s.addView(p);
    return s;
  }
}
