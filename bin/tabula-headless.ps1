# Launch Tabula kernel.
$env:TABULA_HOME = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
& (Join-Path $env:TABULA_HOME "bin" "tabula.exe")
