<?php defined('SYSPATH') or die('No direct script access.');

class Controller_Pages extends Controller {

    public function action_about()
    {
        // Vars passed directly in the chain so the indexer can infer types.
        $view = View::factory('pages/about')
            ->set('user', ORM::factory('User'))
            ->set('show_contact', TRUE)
            ->set('email', 'hello@example.com');
        $this->response->body($view);
    }

    public function action_about_new()
    {
        $user = ORM::factory('User')->find(1);
        $view = new View('pages/about', [
            'user'         => $user,
            'show_contact' => FALSE,
            'email'        => '',
        ]);
        $this->response->body($view);
    }
}
