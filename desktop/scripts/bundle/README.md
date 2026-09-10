## What is this folder
This folder is here to organize all scripts that bundle our source code into a distributable format.
By this, I mean that these scripts will take the compiled binaries and any necessary resources, and package them together for easy distribution and deployment.

`scripts/bundle/dev.ps1` will bundle the source code, for a development environment.
while `scripts/bundle/prod.ps1` will bundle the source code for a production environment, which is an distribution package.