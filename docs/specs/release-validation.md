# Core release validation

All fork build/publication jobs require successful Linux functional validation. Run audit/domain formatting, dispatcher, limiter and real traffic-log tests plus race checks before matrix build jobs can publish assets. Tests must exercise current sources; failures block packaging/publication. This reduces release regressions but does not guarantee absence of deployment-specific faults.
