program VisualTest;

{$mode objfpc}{$H+}

// omniviz capture glue — LCL (Lazarus) port of the Delphi VCL capture in
// examples/delphi-capture. Same contract: build a form, paint
// deterministically, capture the form (GetFormImage), save a PNG into
// $OMNIVIZ_OUTPUT, exit. GetFormImage is the shared primitive — verifying
// it under LCL verifies the pattern Delphi VCL users rely on.
//
//   lazbuild VisualTest.lpi
//   xvfb-run -a ./VisualTest --variant=clean

uses
  {$IFDEF UNIX}cthreads,{$ENDIF}
  Interfaces, // LCL widgetset
  Classes,
  SysUtils,
  Forms,
  Graphics,
  Controls,
  LCLType;

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
  // accept both "--variant clean" and "--variant=clean"
  Result := ADefault;
  for I := 1 to ParamCount do
  begin
    if ParamStr(I) = AName then
      Exit(ParamStr(I + 1));
    if Pos(AName + '=', ParamStr(I)) = 1 then
      Exit(Copy(ParamStr(I), Length(AName) + 2, MaxInt));
  end;
end;

function Col(R, G, B: Byte): TColor; inline;
begin
  Result := TColor(R or (G shl 8) or (B shl 16));
end;

procedure ScaleColor(var C: TColor);
var
  R, G, B: Byte;
begin
  if GetArg('--variant', 'clean') <> 'drift' then
    Exit;
  R := Byte(C and $FF);
  G := Byte((C shr 8) and $FF);
  B := Byte((C shr 16) and $FF);
  if R > 242 then R := 255 else R := (R * 105) div 100;
  if G > 242 then G := 255 else G := (G * 105) div 100;
  if B > 242 then B := 255 else B := (B * 105) div 100;
  C := Col(R, G, B);
end;

{ TCaptureForm }

constructor TCaptureForm.Create(AOwner: TComponent; const AVariant: string);
begin
  inherited CreateNew(AOwner);
  FVariant := AVariant;
  Caption := 'omniviz delphi/lcl capture';
  Width := 656;
  Height := 439;
  Color := Col(18, 20, 28);
  Position := poDesigned;
  Left := 0;
  Top := 0;
  OnPaint := @PaintDashboard;
end;

procedure TCaptureForm.PaintDashboard(Sender: TObject);
var
  Panel, Edge, Accent, Blue, Good: TColor;
  Bars: array[0..5] of Integer;
  I, H, X: Integer;
begin
  Panel := Col(28, 33, 44);    ScaleColor(Panel);
  Edge := Col(48, 54, 61);     ScaleColor(Edge);
  Accent := Col(227, 179, 65); ScaleColor(Accent);
  Blue := Col(88, 166, 255);   ScaleColor(Blue);
  Good := Col(63, 185, 80);    ScaleColor(Good);

  Canvas.Brush.Style := bsSolid;
  Canvas.Pen.Style := psClear;

  // header
  Canvas.Brush.Color := Panel;
  Canvas.Rectangle(0, 0, ClientWidth, 48);
  Canvas.Brush.Color := Accent;
  Canvas.Rectangle(16, 14, 36, 34);

  // bar chart — "broken" collapses one bar (structural regression)
  Bars[0] := 60; Bars[1] := 92; Bars[2] := 45;
  Bars[3] := 120; Bars[4] := 75; Bars[5] := 100;
  if FVariant = 'broken' then
    Bars[3] := 18;

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
    Canvas.Rectangle(X, 340 - H, X + 72, 340);
  end;
  Canvas.Brush.Color := Edge;
  Canvas.Rectangle(16, 342, ClientWidth - 16, 344);
end;

var
  Form: TCaptureForm;
  Img: TBitmap;
  Png: TPortableNetworkGraphic;
  OutDir, VariantName: string;
begin
  VariantName := GetArg('--variant', 'clean');
  OutDir := GetEnvironmentVariable('OMNIVIZ_OUTPUT');
  if OutDir = '' then
    OutDir := '.';
  ForceDirectories(OutDir);

  Application.Initialize;
  Application.MainFormOnTaskbar := False;
  Form := TCaptureForm.Create(nil, VariantName);
  Form.Show;
  Application.ProcessMessages; // let the paint land

  Img := Form.GetFormImage;
  try
    Png := TPortableNetworkGraphic.Create;
    try
      Png.Assign(Img);
      Png.SaveToFile(IncludeTrailingPathDelimiter(OutDir) + 'delphi.png');
    finally
      Png.Free;
    end;
  finally
    Img.Free;
  end;
  Form.Free;
end.
