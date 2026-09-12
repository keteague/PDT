package printer

import "context"

// DeployAll runs deployer.Deploy for every request in order, always
// continuing to the next row regardless of an earlier row's outcome - a
// failure to create one row's printer object is fatal for that row alone
// (see DeployResult.Err), never for the run as a whole. The only thing that
// stops the run early is the caller canceling ctx (e.g. the user closing the
// app mid-deploy); a row already in progress still finishes, but no further
// row is started.
func DeployAll(ctx context.Context, deployer Deployer, reqs []DeployRequest, confirm Confirm) []DeployResult {
	return DeployAllWithProgress(ctx, deployer, reqs, confirm, nil)
}

// DeployAllWithProgress is DeployAll but also invokes onResult immediately
// after each row finishes, in addition to returning every result at the end -
// for a caller (the Wails app) that wants to stream live progress to a UI
// rather than wait for a run of many rows (some taking minutes each) to
// finish before showing anything.
func DeployAllWithProgress(ctx context.Context, deployer Deployer, reqs []DeployRequest, confirm Confirm, onResult func(DeployResult)) []DeployResult {
	if bp, ok := deployer.(BatchPreparer); ok {
		bp.PrepareBatch(ctx, reqs, confirm)
	}

	results := make([]DeployResult, 0, len(reqs))
	for _, req := range reqs {
		if ctx.Err() != nil {
			break
		}
		result := deployer.Deploy(ctx, req, confirm)
		if onResult != nil {
			onResult(result)
		}
		results = append(results, result)
	}
	return results
}
