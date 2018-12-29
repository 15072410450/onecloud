package storageman

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"yunion.io/x/log"

	"yunion.io/x/onecloud/pkg/hostman/guestfs"
	"yunion.io/x/onecloud/pkg/util/qemutils"
)

const MAX_TRIES = 3

type SKVMGuestDisk struct {
	imagePath  string
	nbdDev     string
	partitions []*guestfs.SKVMGuestDiskPartition
}

func NewKVMGuestDisk(imagePath string) *SKVMGuestDisk {
	var ret = new(SKVMGuestDisk)
	ret.imagePath = imagePath
	ret.partitions = make([]*guestfs.SKVMGuestDiskPartition, 0)
	return ret
}

func (d *SKVMGuestDisk) Connect() bool {
	d.nbdDev = nbdManager.AcquireNbddev()
	if len(d.nbdDev) == 0 {
		log.Errorln("Cannot get nbd device")
		return false
	}

	var cmd []string
	if strings.HasPrefix(d.imagePath, "rbd:") || d.getImageFormat() == "raw" {
		cmd = []string{qemutils.GetQemuNbd(), "-c", d.nbdDev, "-f", "raw", d.imagePath}
	} else {
		cmd = []string{qemutils.GetQemuNbd(), "-c", d.nbdDev, d.imagePath}
	}
	_, err := exec.Command(cmd[0], cmd[1:]...).Output()
	if err != nil {
		log.Errorln(err.Error())
		return false
	}

	var tried uint = 0
	for len(d.partitions) == 0 && tried < MAX_TRIES {
		time.Sleep((1 << tried) * time.Second)
		err = d.findPartitions()
		if err != nil {
			log.Errorln(err.Error())
			return false
		}
		tried += 1
	}
	d.setupLVMS()
	return true
}

func (d *SKVMGuestDisk) getImageFormat() string {
	lines, err := exec.Command(qemutils.GetQemuImg(), "info", d.imagePath).Output()
	if err != nil {
		return ""
	}
	imgStr := strings.Split(string(lines), "\n")
	for i := 0; i < len(imgStr); i++ {
		if strings.HasPrefix(imgStr[i], "file format: ") {
			return imgStr[i][len("file format: "):]
		}
	}
	return ""
}

func (d *SKVMGuestDisk) findPartitions() error {
	if len(d.nbdDev) == 0 {
		return fmt.Errorf("Want find partitions but dosen't have nbd dev")
	}
	dev := filepath.Base(d.nbdDev)
	devpath := filepath.Dir(d.nbdDev)
	files, err := ioutil.ReadDir(devpath)
	if err != nil {
		return err
	}
	var partitions []*guestfs.SKVMGuestDiskPartition
	for i := 0; i < len(files); i++ {
		if files[i].Name() != dev && strings.HasPrefix(files[i].Name(), dev+"p") {
			var part = guestfs.NewKVMGuestDiskPartition(path.Join(devpath, files[i].Name()))
			d.partitions = append(d.partitions, part)
		}
	}
	return nil
}

func (d *SKVMGuestDisk) setupLVMS() error {
	//TODO?? 可能不需要开发这里
	return fmt.Errorf("not implement right now")
}

func (d *SKVMGuestDisk) Disconnect() bool {
	if len(d.nbdDev) > 0 {
		// TODO?? PutdownLVMS ??
		err := exec.Command(qemutils.GetQemuNbd(), "-d", d.nbdDev).Run()
		if err != nil {
			log.Errorln(err.Error())
			return false
		}
		nbdManager.ReleaseNbddev(d.nbdDev)
		d.nbdDev = ""
		d.partitions = d.partitions[len(d.partitions):]
		return true
	} else {
		return false
	}
}

func (d *SKVMGuestDisk) Mount() guestfs.IRootFsDriver {
	for i := 0; i < len(d.partitions); i++ {
		if d.partitions[i].Mount() {
			if fs := guestfs.DetectRootFs(d.partitions[i]); fs != nil {
				log.Infof("Use rootfs %s", fs)
				return fs
			} else {
				d.partitions[i].Umount()
			}
		}
	}
	return nil
}

func (d *SKVMGuestDisk) Umount(fd guestfs.IRootFsDriver) {
	if part := fd.GetPartition(); part != nil {
		part.Umount()
	}
}
