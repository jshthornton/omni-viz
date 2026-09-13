// CaptureApp (Mono variant) — same capture contract as the .NET version,
// but written for mono's C# compiler (classic namespaces, no nullable
// annotations) so the WinForms capture path can be verified on Linux via
// Mono's System.Windows.Forms implementation. See docker/winforms.
//
//   mcs -r:System.Windows.Forms -r:System.Drawing Program.cs
//   xvfb-run -a mono CaptureApp.exe --set=dashboard --variant=clean
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
using System.Windows.Forms;

namespace CaptureApp
{
    internal static class Program
    {
        [STAThread]
        static int Main(string[] args)
        {
            string variant = GetArg(args, "--variant", "clean");
            string set = GetArg(args, "--set", "dashboard");
            string outputDir = Environment.GetEnvironmentVariable("OMNIVIZ_OUTPUT");
            if (String.IsNullOrEmpty(outputDir)) outputDir = ".";

            Directory.CreateDirectory(outputDir);
            Application.EnableVisualStyles();

            using (CaptureForm form = new CaptureForm(variant, set))
            {
                form.Show();
                Application.DoEvents();
                form.CaptureTo(Path.Combine(outputDir, set + ".png"));
            }
            return 0;
        }

        static string GetArg(string[] args, string name, string fallback)
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
        string variant;
        string set;

        public CaptureForm(string variant, string set)
        {
            this.variant = variant;
            this.set = set;
            Text = "omniviz winforms capture";
            ClientSize = new Size(640, 400);
            FormBorderStyle = FormBorderStyle.FixedSingle;
            StartPosition = FormStartPosition.Manual;
            Location = new Point(0, 0);
            BackColor = Color.FromArgb(18, 20, 28);
        }

        public void CaptureTo(string path)
        {
            using (Bitmap bmp = new Bitmap(ClientSize.Width, ClientSize.Height))
            {
                DrawToBitmap(bmp, new Rectangle(Point.Empty, bmp.Size));
                bmp.Save(path, ImageFormat.Png);
            }
        }

        protected override void OnPaint(PaintEventArgs e)
        {
            base.OnPaint(e);
            Graphics g = e.Graphics;

            Color bg = C(18, 20, 28), panel = C(28, 33, 44), edge = C(48, 54, 61);
            Color accent = C(227, 179, 65), blue = C(88, 166, 255), good = C(63, 185, 80);

            g.FillRectangle(new SolidBrush(panel), 0, 0, ClientSize.Width, 48);
            g.FillRectangle(new SolidBrush(accent), 16, 14, 20, 20);

            if (set == "dashboard")
            {
                string[][] cards = new string[][] {
                    new string[] { "requests/s", "1,284" },
                    new string[] { "errors", "12" },
                    new string[] { "p95", "212ms" },
                };
                int n = variant == "broken" ? 2 : 3;
                Font small = new Font(FontFamily.GenericSansSerif, 9);
                Font big = new Font(FontFamily.GenericSansSerif, 16, FontStyle.Bold);
                Brush dim = new SolidBrush(C(139, 148, 158));
                Brush fg = new SolidBrush(C(230, 237, 243));
                for (int i = 0; i < n; i++)
                {
                    Rectangle r = new Rectangle(16 + i * 204, 64, 188, 84);
                    g.FillRectangle(new SolidBrush(panel), r);
                    g.DrawRectangle(new Pen(edge), r);
                    g.DrawString(cards[i][0], small, dim, r.X + 10, r.Y + 8);
                    g.DrawString(cards[i][1], big, fg, r.X + 10, r.Y + 32);
                }

                int[] bars = new int[] { 60, 92, 45, 120, 75, 100 };
                if (variant == "broken") bars[3] = 18;
                Color[] colors = new Color[] { blue, good, accent };
                for (int i = 0; i < bars.Length; i++)
                {
                    int x = 16 + i * 100, h = bars[i];
                    g.FillRectangle(new SolidBrush(colors[i % 3]), x, 340 - h, 72, h);
                }
                g.FillRectangle(new SolidBrush(edge), 16, 342, ClientSize.Width - 32, 2);
            }
            else
            {
                Rectangle r = new Rectangle(16, 64, ClientSize.Width - 32, 200);
                g.FillRectangle(new SolidBrush(panel), r);
                g.DrawRectangle(new Pen(edge), r);
                g.FillPie(new SolidBrush(accent), r.X + 20, r.Y + 20, 160, 160, -90, 270);
                g.FillPie(new SolidBrush(blue), r.X + 240, r.Y + 20, 160, 160, -90, 130);
            }
        }

        Color C(byte r, byte gg, byte b)
        {
            if (variant != "drift") return Color.FromArgb(r, gg, b);
            return Color.FromArgb(Scale(r), Scale(gg), Scale(b));
        }

        static byte Scale(byte v)
        {
            int x = v * 105 / 100;
            return x > 255 ? (byte)255 : (byte)x;
        }
    }
}
