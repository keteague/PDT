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
	results := make([]DeployResult, 0, len(reqs))
	for _, req := range reqs {
		if ctx.Err() != nil {
			break
		}
		results = append(results, deployer.Deploy(ctx, req, confirm))
	}
	return results
}
