program VisualTest;

{$APPTYPE CONSOLE}

// omniviz capture glue for Delphi VCL applications.
//
// Builds a form in code (no .dfm), paints a deterministic dashboard in
// OnPaint, captures the form via GetFormImage and saves a PNG into
// $OMNIVIZ_OUTPUT — then exits. The same three lines (GetFormImage →
// TPNGImage.Assign → SaveToFile) work inside any existing VCL screen.
//
//   compile (Delphi 11+):  dcc32 -B VisualTest.dpr
//   variants:              --variant=clean|drift|broken
//
// For FMX: TControl.MakeScreenshot(TBitmap) is the equivalent of
// GetFormImage; the rest is identical.

uses
  System.SysUtils,
  System.Types,
  System.UITypes,
  System.Classes,
  System.IOUtils,
  Vcl.Forms,
  Vcl.Graphics,
  Vcl.Controls,
  Vcl.Imaging.pngimage;

type
  TCaptureForm = class(TForm)
  private
    FVariant: string;
    procedure PaintDashboard(Sender: TObject);
  public
    constructor Create(AOwner: TComponent; const AVariant: string);
  end;

function GetArg(const AName, ADefault: string): string;
var
  I: Integer;
begin
  Result := ADefault;
  for I := 1 to ParamCount - 1 do
    if ParamStr(I) = AName then
      Exit(ParamStr(I + 1));
end;

procedure ScaleColor(var C: TColor);
var
  R, G, B: Byte;
begin
  // drift: every color ~5% brighter → trips the changed-area budget only
  if GetArg('--variant', 'clean') <> 'drift' then
    Exit;
  R := GetRValue(C);
  G := GetGValue(C);
  B := GetBValue(C);
  if (R * 105) div 100 > 255 then R := 255 else R := (R * 105) div 100;
  if (G * 105) div 100 > 255 then G := 255 else G := (G * 105) div 100;
  if (B * 105) div 100 > 255 then B := 255 else B := (B * 105) div 100;
  C := RGB(R, G, B);
end;

{ TCaptureForm }

constructor TCaptureForm.Create(AOwner: TComponent; const AVariant: string);
begin
  inherited CreateNew(AOwner);
  FVariant := AVariant;
  Caption := 'omniviz delphi capture';
  ClientWidth := 640;
  ClientHeight := 400;
  Color := RGB(18, 20, 28);
  Position := poDesigned;
  Left := 0;
  Top := 0;
  BorderStyle := bsSingle;
  OnPaint := PaintDashboard;
end;

procedure TCaptureForm.PaintDashboard(Sender: TObject);
var
  Panel, Edge, Accent, Blue, Good: TColor;
  Bars: array[0..5] of Integer;
  I, H, X: Integer;

  procedure Rect(const R: TRect; C: TColor);
  begin
    Canvas.Brush.Color := C;
    Canvas.Brush.Style := bsSolid;
    Canvas.Pen.Style := psClear;
    Canvas.Rectangle(R.Left, R.Top, R.Right, R.Bottom);
  end;

begin
  Panel := RGB(28, 33, 44);   ScaleColor(Panel);
  Edge := RGB(48, 54, 61);    ScaleColor(Edge);
  Accent := RGB(227, 179, 65); ScaleColor(Accent);
  Blue := RGB(88, 166, 255);  ScaleColor(Blue);
  Good := RGB(63, 185, 80);   ScaleColor(Good);

  // header
  Rect(TRect.Create(0, 0, ClientWidth, 48), Panel);
  Rect(TRect.Create(16, 14, 36, 34), Accent);

  // bar chart — "broken" collapses one bar, the classic structural regression
  Bars[0] := 60; Bars[1] := 92; Bars[2] := 45;
  Bars[3] := 120; Bars[4] := 75; Bars[5] := 100;
  if FVariant = 'broken' then
    Bars[3] := 18;

  Canvas.Pen.Style := psClear;
  for I := 0 to High(Bars) do
  begin
    X := 16 + I * 100;
    H := Bars[I];
    case I mod 3 of
      0: Canvas.Brush.Color := Blue;
      1: Canvas.Brush.Color := Good;
    else
      Canvas.Brush.Color := Accent;
    end;
    Canvas.Brush.Style := bsSolid;
    Canvas.Rectangle(X, 340 - H, X + 72, 340);
  end;
  Rect(TRect.Create(16, 342, ClientWidth - 16, 344), Edge);
end;

var
  Form: TCaptureForm;
  Img: TBitmap;
  Png: TPNGImage;
  OutDir, VariantName: string;
begin
  try
    VariantName := GetArg('--variant', 'clean');
    OutDir := GetEnvironmentVariable('OMNIVIZ_OUTPUT');
    if OutDir = '' then
      OutDir := '.';
    ForceDirectories(OutDir);

    Application.Initialize;
    Application.MainFormOnTaskbar := False;
    Form := TCaptureForm.Create(nil, VariantName);
    Form.Show;
    Application.ProcessMessages; // let WM_PAINT land

    Img := Form.GetFormImage;
    try
      Png := TPNGImage.Create;
      try
        Png.Assign(Img);
        Png.SaveToFile(TPath.Combine(OutDir, 'delphi.png'));
      finally
        Png.Free;
      end;
    finally
      Img.Free;
    end;
    Form.Free;
  except
    on E: Exception do
    begin
      Writeln('omniviz-delphi: ', E.Message);
      ExitCode := 1;
    end;
  end;
end.
