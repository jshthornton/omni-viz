// CaptureApp — omniviz capture glue for .NET WinForms.
//
// The form paints a deterministic dashboard in OnPaint (no timers, no
// random), then captures ITSELF via DrawToBitmap and saves to
// $OMNIVIZ_OUTPUT — the exact same trick works for any WinForms/WPF screen:
// point DrawToBitmap at the control you want, save, exit.
//
// Variants demonstrate the two failure classes VRT catches:
//   --variant=clean    (default) baseline render
//   --variant=drift    every brush color brightened ~5% → changed-area budget failure
//   --variant=broken   a panel removed, a bar collapsed → per-pixel failure
//
// Windows only (WinForms). Build: dotnet build -c Release
using System.Diagnostics;

namespace CaptureApp;

internal static class Program
{
    [STAThread]
    private static int Main(string[] args)
    {
        string variant = GetArg(args, "--variant", "clean");
        string set = GetArg(args, "--set", "dashboard");
        string outputDir =
            Environment.GetEnvironmentVariable("OMNIVIZ_OUTPUT")
            ?? Path.Combine(".", "tmp");

        Directory.CreateDirectory(outputDir);
        Application.EnableVisualStyles();
        Application.SetCompatibleTextRenderingDefault(false);

        using var form = new CaptureForm(variant, set);
        form.Show();
        // One pump so WM_PAINT lands, then capture the pixels.
        Application.DoEvents();
        form.CaptureTo(Path.Combine(outputDir, set + ".png"));
        return 0;
    }

    private static string GetArg(string[] args, string name, string fallback)
    {
        // accept both "--variant clean" and "--variant=clean"
        for (int i = 0; i < args.Length; i++)
        {
            if (args[i] == name && i + 1 < args.Length) return args[i + 1];
            if (args[i].StartsWith(name + "=")) return args[i].Substring(name.Length + 1);
        }
        return fallback;
    }
}

public sealed class CaptureForm : Form
{
    private readonly string _variant;
    private readonly string _set;

    public CaptureForm(string variant, string set)
    {
        _variant = variant;
        _set = set;
        Text = "omniviz winforms capture";
        ClientSize = new Size(640, 400);
        FormBorderStyle = FormBorderStyle.FixedSingle;
        StartPosition = FormStartPosition.Manual;
        Location = new Point(0, 0);
        BackColor = Color.FromArgb(18, 20, 28);
    }

    public void CaptureTo(string path)
    {
        using var bmp = new Bitmap(ClientSize.Width, ClientSize.Height);
        DrawToBitmap(bmp, new Rectangle(Point.Empty, bmp.Size));
        bmp.Save(path, System.Drawing.Imaging.ImageFormat.Png);
    }

    protected override void OnPaint(PaintEventArgs e)
    {
        base.OnPaint(e);
        var g = e.Graphics;
        g.SmoothingMode = System.Drawing.Drawing2D.SmoothingMode.None; // deterministic

        // drift: scale every color ~5% — trips the changed-area budget only
        Color C(byte r, byte gg, byte b) =>
            _variant == "drift"
                ? Color.FromArgb(Scale(r), Scale(gg), Scale(b))
                : Color.FromArgb(r, gg, b);
        static byte Scale(byte v) => (byte)Math.Min(255, v * 1.05);

        Color bg = C(22, 27, 39), panel = C(28, 33, 44), edge = C(48, 54, 61);
        Color accent = C(227, 179, 65), blue = C(88, 166, 255), good = C(63, 185, 80);

        // header
        g.FillRectangle(new SolidBrush(panel), 0, 0, ClientSize.Width, 48);
        g.FillRectangle(new SolidBrush(accent), 16, 14, 20, 20);

        if (_set == "dashboard")
        {
            // three stat cards — the "broken" variant loses one
            var cards = new[] { ("requests/s", "1,284"), ("errors", "12"), ("p95", "212ms") };
            int n = _variant == "broken" ? 2 : 3;
            for (int i = 0; i < n; i++)
            {
                var r = new Rectangle(16 + i * 204, 64, 188, 84);
                g.FillRectangle(new SolidBrush(panel), r);
                g.DrawRectangle(new Pen(edge), r);
                g.DrawString(cards[i].Item1, new Font(FontFamily.GenericSansSerif, 9), Brushes.Gray, r.X + 10, r.Y + 8);
                g.DrawString(cards[i].Item2, new Font(FontFamily.GenericSansSerif, 16, FontStyle.Bold), new SolidBrush(C(230, 237, 243)), r.X + 10, r.Y + 32);
            }

            // bar chart
            int[] bars = { 60, 92, 45, 120, 75, 100 };
            if (_variant == "broken") bars[3] = 18;
            var colors = new[] { blue, good, accent };
            for (int i = 0; i < bars.Length; i++)
            {
                int x = 16 + i * 100, h = bars[i];
                g.FillRectangle(new SolidBrush(colors[i % 3]), x, 340 - h, 72, h);
            }
            g.FillRectangle(new SolidBrush(edge), 16, 342, ClientSize.Width - 32, 2);
        }
        else
        {
            var r = new Rectangle(16, 64, ClientSize.Width - 32, 200);
            g.FillRectangle(new SolidBrush(panel), r);
            g.DrawRectangle(new Pen(edge), r);
            g.FillPie(new SolidBrush(accent), r.X + 20, r.Y + 20, 160, 160, -90, 270);
            g.FillPie(new SolidBrush(blue), r.X + 240, r.Y + 20, 160, 160, -90, 130);
        }
    }
}
