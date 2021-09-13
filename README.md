
# ignition2rpm

# ! Do not depend on this or use this for anything, it is a science experiment !

Tooling to convert ignition to RPM files for use in rpm-ostree images

This really just uses rpmpack ( github.com/google/rpmpack) to package CoreOS's ignition format into an RPM file

```
ignition2rpm --config /home/jkyros/dev/ignition/machine_config.ign --output ./machine-config-1.rpm
```