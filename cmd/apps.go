package cmd

import logging "github.com/sirupsen/logrus"

func getDelegationApplications(a *CrossmeshApplication, log *logging.Entry) []delegationApplication {
	// apps to load.
	return []delegationApplication{
		&versionPrintApp{}, // version printing.
		newCoreDaemonApplication(),
	}
}
