<?php defined('SYSPATH') or die('No direct script access.');

class Controller_Pages extends Controller {

    public function action_about()
    {
        // This view name exists in both the blog module and (if present) application.
        // go-to-definition should return both candidates.
        $view = View::factory('pages/about')
            ->set('author', ORM::factory('User')->find(1));
        $this->response->body($view);
    }

    public function action_find()
    {
        // Kohana::find_file variant — also indexed as a view reference.
        $path = Kohana::find_file('views', 'pages/about');
        $this->response->body(View::factory('pages/about'));
    }

    public function action_admin_widget()
    {
        // HMVC sub-request into Controller_Admin_Users::action_edit.
        $body = Request::factory()->directory('admin')->controller('users')->action('edit')->execute()->body();
        $this->response->body($body);
    }
}
